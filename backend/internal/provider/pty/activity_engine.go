package pty

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

const defaultOpenWindow = 8

type ActivityEntry struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	TS        time.Time      `json:"ts"`
	Rev       int            `json:"rev"`
	Open      bool           `json:"open"`
	Data      map[string]any `json:"data,omitempty"`
}

type ActivityRecord struct {
	Type  string         `json:"type"`
	Entry *ActivityEntry `json:"entry,omitempty"`
	ID    string         `json:"id,omitempty"`
	Rev   int            `json:"rev,omitempty"`
	TS    time.Time      `json:"ts,omitempty"`
}

type ScreenState struct {
	Rows  int
	Cols  int
	Lines []string
}

func (s *ScreenState) ApplySnapshot(snap terminal.Snapshot) {
	if snap.Rows <= 0 || snap.Cols <= 0 {
		return
	}
	s.Rows = snap.Rows
	s.Cols = snap.Cols
	s.Lines = append([]string(nil), snap.Lines...)
}

func (s *ScreenState) ApplyDiff(diff terminal.Diff) {
	if diff.Region.Y2 <= diff.Region.Y {
		return
	}
	if s.Rows == 0 || s.Cols == 0 {
		if diff.Region.Y2 > 0 {
			s.Rows = diff.Region.Y2
		}
	}
	if len(s.Lines) < s.Rows {
		s.Lines = append(s.Lines, make([]string, s.Rows-len(s.Lines))...)
	}
	for i := 0; i < len(diff.Lines); i++ {
		row := diff.Region.Y + i
		if row < 0 {
			continue
		}
		if row >= len(s.Lines) {
			s.Lines = append(s.Lines, make([]string, row-len(s.Lines)+1)...)
		}
		s.Lines[row] = diff.Lines[i]
	}
}

func (s *ScreenState) RegionText(region RegionSpec) string {
	if s == nil || len(s.Lines) == 0 {
		return ""
	}
	top, bottom, left, right := resolveRegionBounds(region, s.Rows, s.Cols)
	if bottom <= top || right <= left {
		return ""
	}
	if top < 0 {
		top = 0
	}
	if bottom > len(s.Lines) {
		bottom = len(s.Lines)
	}
	var builder strings.Builder
	for y := top; y < bottom; y++ {
		line := ""
		if y < len(s.Lines) {
			line = s.Lines[y]
		}
		slice := sliceLine(line, left, right)
		slice = strings.TrimRight(slice, " \t")
		builder.WriteString(slice)
		if y < bottom-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

type ActivityEmitter struct {
	sessionID    string
	log          io.Writer
	state        *ExtractorState
	openWindow   int
	emitMetadata func(string, any)
}

func NewActivityEmitter(sessionID string, log io.Writer, state *ExtractorState, openWindow int, emitMetadata func(string, any)) *ActivityEmitter {
	if openWindow <= 0 {
		openWindow = defaultOpenWindow
	}
	if state == nil {
		state = &ExtractorState{}
	}
	if state.EntryRevisions == nil {
		state.EntryRevisions = make(map[string]int)
	}
	return &ActivityEmitter{
		sessionID:    sessionID,
		log:          log,
		state:        state,
		openWindow:   openWindow,
		emitMetadata: emitMetadata,
	}
}

func (e *ActivityEmitter) Upsert(kind string, data map[string]any, entryID string, open bool) (*ActivityEntry, error) {
	if entryID == "" {
		return nil, errors.New("missing entry id")
	}
	rev := e.nextRevision(entryID)
	entry := &ActivityEntry{
		ID:        entryID,
		SessionID: e.sessionID,
		Kind:      kind,
		TS:        time.Now(),
		Rev:       rev,
		Open:      open,
		Data:      data,
	}
	if open {
		e.addOpenEntry(entryID)
	} else {
		e.removeOpenEntry(entryID)
	}
	if err := AppendActivityRecord(e.log, ActivityRecord{Type: "entry.upsert", Entry: entry}); err != nil {
		return nil, err
	}
	e.state.UpdatedAt = time.Now()
	return entry, nil
}

func (e *ActivityEmitter) Finalize(entryID string) error {
	if entryID == "" {
		return errors.New("missing entry id")
	}
	rev := e.nextRevision(entryID)
	e.removeOpenEntry(entryID)
	record := ActivityRecord{
		Type: "entry.finalize",
		ID:   entryID,
		Rev:  rev,
		TS:   time.Now(),
	}
	if err := AppendActivityRecord(e.log, record); err != nil {
		return err
	}
	e.state.UpdatedAt = time.Now()
	return nil
}

func (e *ActivityEmitter) State() *ExtractorState {
	return e.state
}

func (e *ActivityEmitter) IsOpen(entryID string) bool {
	if entryID == "" {
		return false
	}
	for _, id := range e.state.OpenEntries {
		if id == entryID {
			return true
		}
	}
	return false
}

func (e *ActivityEmitter) HasRevision(entryID string) bool {
	if entryID == "" {
		return false
	}
	_, ok := e.state.EntryRevisions[entryID]
	return ok
}

func (e *ActivityEmitter) nextRevision(entryID string) int {
	rev := e.state.EntryRevisions[entryID] + 1
	e.state.EntryRevisions[entryID] = rev
	return rev
}

func (e *ActivityEmitter) addOpenEntry(entryID string) {
	if entryID == "" {
		return
	}
	e.removeOpenEntry(entryID)
	e.state.OpenEntries = append(e.state.OpenEntries, entryID)
	if len(e.state.OpenEntries) > e.openWindow {
		overflow := e.state.OpenEntries[0]
		e.state.OpenEntries = e.state.OpenEntries[1:]
		if err := e.Finalize(overflow); err != nil {
			e.emitWarning("open_window_overflow", map[string]any{"entry_id": overflow, "error": err.Error()})
		}
	}
}

func (e *ActivityEmitter) removeOpenEntry(entryID string) {
	if entryID == "" {
		return
	}
	for i, id := range e.state.OpenEntries {
		if id == entryID {
			e.state.OpenEntries = append(e.state.OpenEntries[:i], e.state.OpenEntries[i+1:]...)
			return
		}
	}
}

func (e *ActivityEmitter) emitWarning(code string, fields map[string]any) {
	if e.emitMetadata == nil {
		return
	}
	payload := map[string]any{"code": code}
	for k, v := range fields {
		payload[k] = v
	}
	e.emitMetadata("extractor_warning", payload)
}

type ScreenDiffExtractor struct {
	profile *CompiledProfile
	screen  *ScreenState
	emitter *ActivityEmitter
}

func NewScreenDiffExtractor(profile *CompiledProfile, emitter *ActivityEmitter) *ScreenDiffExtractor {
	return &ScreenDiffExtractor{
		profile: profile,
		screen:  &ScreenState{},
		emitter: emitter,
	}
}

func (e *ScreenDiffExtractor) HandleUpdate(update terminal.Update) error {
	if e == nil || e.profile == nil || e.emitter == nil {
		return nil
	}
	switch update.Kind {
	case terminal.UpdateSnapshot:
		if update.Snapshot == nil {
			return nil
		}
		e.screen.ApplySnapshot(*update.Snapshot)
		full := RegionSpec{Top: intPtr(0), Bottom: intPtr(update.Snapshot.Rows)}
		return e.applyRules(full, terminal.Region{X: 0, Y: 0, X2: update.Snapshot.Cols, Y2: update.Snapshot.Rows})
	case terminal.UpdateDiff:
		if update.Diff == nil {
			return nil
		}
		e.screen.ApplyDiff(*update.Diff)
		return e.applyRules(RegionSpec{}, update.Diff.Region)
	default:
		return nil
	}
}

func (e *ScreenDiffExtractor) applyRules(full RegionSpec, changed terminal.Region) error {
	for _, rule := range e.profile.Rules {
		if !rule.Enabled {
			continue
		}
		if !triggerMatches(rule.Trigger, changed) {
			continue
		}
		extractRegion := rule.Extract.Region
		if extractRegion.Top == nil && extractRegion.Bottom == nil {
			extractRegion = full
		}
		text := e.screen.RegionText(extractRegion)
		if text == "" {
			continue
		}
		data, matchKey, ok := extractRuleData(rule, text)
		if !ok {
			continue
		}
		if data == nil {
			data = map[string]any{}
		}
		addRegionData(data, extractRegion, e.screen)
		entryID := buildEntryID(rule, matchKey)
		if rule.Emit.UpdateWindow == "recent_open" && e.emitter.HasRevision(entryID) && !e.emitter.IsOpen(entryID) {
			continue
		}
		open := true
		if rule.Emit.Open != nil {
			open = *rule.Emit.Open
		}
		if _, err := e.emitter.Upsert(rule.Emit.Kind, data, entryID, open); err != nil {
			return err
		}
		if rule.Emit.Finalize {
			if err := e.emitter.Finalize(entryID); err != nil {
				return err
			}
		}
	}
	return nil
}

func triggerMatches(trigger RegionTrigger, changed terminal.Region) bool {
	triggerRegion := regionFromTrigger(trigger)
	return regionsIntersect(triggerRegion, changed)
}

func extractRuleData(rule CompiledRule, text string) (map[string]any, string, bool) {
	switch rule.Extract.Type {
	case "region_text":
		return map[string]any{"text": strings.TrimSpace(text)}, "", true
	case "region_regex":
		if rule.Regex == nil {
			return nil, "", false
		}
		matches := rule.Regex.FindStringSubmatch(text)
		if matches == nil {
			return nil, "", false
		}
		data := map[string]any{}
		key := ""
		for i, name := range rule.Regex.SubexpNames() {
			if i == 0 || name == "" {
				continue
			}
			if i >= len(matches) {
				continue
			}
			data[name] = matches[i]
			if name == "id" || name == "key" || name == rule.Identity.Capture {
				if matches[i] != "" {
					key = matches[i]
				}
			}
		}
		if len(data) == 0 && len(matches) > 0 {
			data["text"] = strings.TrimSpace(matches[0])
		}
		if rule.Identity.Static != "" {
			key = rule.Identity.Static
		}
		return data, key, true
	default:
		return nil, "", false
	}
}

func buildEntryID(rule CompiledRule, key string) string {
	identity := strings.TrimSpace(key)
	if identity == "" {
		identity = rule.ID
	}
	hash := sha1.Sum([]byte(rule.ID + ":" + identity))
	return "act_" + hex.EncodeToString(hash[:8])
}

func regionsIntersect(a, b terminal.Region) bool {
	if a.X2 <= a.X || a.Y2 <= a.Y {
		return false
	}
	if b.X2 <= b.X || b.Y2 <= b.Y {
		return false
	}
	return a.X < b.X2 && a.X2 > b.X && a.Y < b.Y2 && a.Y2 > b.Y
}

func regionFromTrigger(trigger RegionTrigger) terminal.Region {
	left := 0
	right := 1 << 30
	if trigger.Left != nil {
		left = *trigger.Left
	}
	if trigger.Right != nil {
		right = *trigger.Right
	}
	return terminal.Region{X: left, Y: trigger.Top, X2: right, Y2: trigger.Bottom}
}

func resolveRegionBounds(region RegionSpec, rows, cols int) (int, int, int, int) {
	top := 0
	bottom := rows
	left := 0
	right := cols
	if region.Top != nil {
		top = *region.Top
	}
	if region.Bottom != nil {
		bottom = *region.Bottom
	}
	if region.Left != nil {
		left = *region.Left
	}
	if region.Right != nil {
		right = *region.Right
	}
	return top, bottom, left, right
}

func sliceLine(line string, left, right int) string {
	if left < 0 {
		left = 0
	}
	if right < left {
		return ""
	}
	runes := []rune(line)
	if left >= len(runes) {
		return ""
	}
	if right > len(runes) {
		right = len(runes)
	}
	return string(runes[left:right])
}

func addRegionData(data map[string]any, region RegionSpec, screen *ScreenState) {
	if data == nil {
		return
	}
	if _, ok := data["region"]; ok {
		return
	}
	top, bottom, left, right := resolveRegionBounds(region, screen.Rows, screen.Cols)
	data["region"] = map[string]int{
		"top":    top,
		"bottom": bottom,
		"left":   left,
		"right":  right,
	}
}

func intPtr(v int) *int {
	return &v
}
