package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) getExtractorConfig(w http.ResponseWriter, r *http.Request) {
	config, err := pty.LoadRuleConfig(pty.DefaultRulesPath())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid extractor config", err.Error())
		return
	}
	response := apiTypes.ExtractorConfigResponse{
		Config: apiTypes.ExtractorConfig{Version: 1, Profiles: []apiTypes.ExtractorProfile{}},
		Valid:  false,
		Exists: false,
	}
	if config == nil {
		response.Errors = []string{"config file not found"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	response.Config = ruleConfigToAPI(config)
	response.Valid = true
	response.Exists = true
	response.Errors = nil
	if err := config.Validate(); err != nil {
		response.Valid = false
		response.Errors = []string{err.Error()}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *Handler) putExtractorConfig(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.ExtractorConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	config := ruleConfigFromAPI(req)
	if err := pty.SaveRuleConfig(pty.DefaultRulesPath(), config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid extractor config", err.Error())
		return
	}
	resp := apiTypes.ExtractorConfigResponse{
		Config: req,
		Valid:  true,
		Exists: true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) validateExtractorConfig(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.ExtractorValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	config := ruleConfigFromAPI(req.Config)
	if err := config.Validate(); err != nil {
		resp := apiTypes.ExtractorValidateResponse{Valid: false, Errors: []string{err.Error()}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	resp := apiTypes.ExtractorValidateResponse{Valid: true}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) replayExtractor(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required", "")
		return
	}

	var req apiTypes.ExtractorReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.ProfileID == "" {
		writeError(w, http.StatusBadRequest, "profile_id is required", "")
		return
	}
	startOffset, err := parseReplayOffset(r, req.StartOffset)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start offset", err.Error())
		return
	}

	var config *pty.RuleConfig
	if req.Config != nil {
		config = ruleConfigFromAPI(*req.Config)
	} else {
		config, err = pty.LoadRuleConfig(pty.DefaultRulesPath())
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid extractor config", err.Error())
			return
		}
	}
	if config == nil {
		writeError(w, http.StatusNotFound, "extractor config not found", "")
		return
	}
	if err := config.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid extractor config", err.Error())
		return
	}

	profile, err := findRuleProfile(config, req.ProfileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found", err.Error())
		return
	}
	compiled, err := pty.CompileProfile(profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile", err.Error())
		return
	}

	var logBuf bytes.Buffer
	emitter := pty.NewActivityEmitter(sessionID, &logBuf, &pty.ExtractorState{}, 8, nil)
	extractor := pty.NewScreenDiffExtractor(compiled, emitter)
	path := pty.PTYLogPath(sessionID)
	offset, diag, err := pty.ReplayActivityFromPTYLog(path, startOffset, extractor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "replay failed", err.Error())
		return
	}
	records, err := decodeActivityRecords(&logBuf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "replay decode failed", err.Error())
		return
	}

	resp := apiTypes.ExtractorReplayResponse{
		Offset:      offset,
		Diagnostics: toAPIDiagnostics(diag),
		Records:     records,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) getTerminalSnapshot(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required", "")
		return
	}
	snapshot, err := h.executor.TerminalSnapshot(sessionID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionNotFound):
			writeError(w, http.StatusNotFound, "session not found", "")
		case errors.Is(err, service.ErrTerminalNotSupported):
			writeError(w, http.StatusBadRequest, "terminal snapshot not supported", "")
		default:
			writeError(w, http.StatusInternalServerError, "failed to get terminal snapshot", err.Error())
		}
		return
	}
	resp := apiTypes.TerminalSnapshot{Rows: snapshot.Rows, Cols: snapshot.Cols, Lines: snapshot.Lines}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseReplayOffset(r *http.Request, bodyOffset *int64) (int64, error) {
	if bodyOffset != nil {
		return *bodyOffset, nil
	}
	from := r.URL.Query().Get("from")
	if from == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(from, 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func decodeActivityRecords(buf *bytes.Buffer) ([]apiTypes.ExtractorActivityRecord, error) {
	if buf == nil || buf.Len() == 0 {
		return []apiTypes.ExtractorActivityRecord{}, nil
	}
	reader := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	reader.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	records := []apiTypes.ExtractorActivityRecord{}
	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var record pty.ActivityRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		records = append(records, toAPIActivityRecord(record))
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func toAPIDiagnostics(diag pty.PTYLogDiagnostics) apiTypes.PTYLogDiagnostics {
	return apiTypes.PTYLogDiagnostics{
		Frames:        diag.Frames,
		Bytes:         diag.Bytes,
		PartialFrame:  diag.PartialFrame,
		PartialOffset: diag.PartialOffset,
		CorruptFrames: diag.CorruptFrames,
		CorruptOffset: diag.CorruptOffset,
	}
}

func ruleConfigFromAPI(cfg apiTypes.ExtractorConfig) *pty.RuleConfig {
	profiles := make([]pty.RuleProfile, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		profiles = append(profiles, ruleProfileFromAPI(profile))
	}
	return &pty.RuleConfig{Version: cfg.Version, Profiles: profiles}
}

func ruleConfigToAPI(cfg *pty.RuleConfig) apiTypes.ExtractorConfig {
	if cfg == nil {
		return apiTypes.ExtractorConfig{Version: 1, Profiles: []apiTypes.ExtractorProfile{}}
	}
	profiles := make([]apiTypes.ExtractorProfile, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		profiles = append(profiles, ruleProfileToAPI(profile))
	}
	return apiTypes.ExtractorConfig{Version: cfg.Version, Profiles: profiles}
}

func ruleProfileFromAPI(profile apiTypes.ExtractorProfile) pty.RuleProfile {
	rules := make([]pty.RuleDefinition, 0, len(profile.Rules))
	for _, rule := range profile.Rules {
		rules = append(rules, ruleDefinitionFromAPI(rule))
	}
	return pty.RuleProfile{
		ID:      profile.ID,
		Enabled: profile.Enabled,
		Match: pty.RuleProfileMatch{
			CommandRegex: profile.Match.CommandRegex,
			ArgsRegex:    profile.Match.ArgsRegex,
		},
		Rules: rules,
	}
}

func ruleProfileToAPI(profile pty.RuleProfile) apiTypes.ExtractorProfile {
	rules := make([]apiTypes.ExtractorRule, 0, len(profile.Rules))
	for _, rule := range profile.Rules {
		rules = append(rules, ruleDefinitionToAPI(rule))
	}
	return apiTypes.ExtractorProfile{
		ID:      profile.ID,
		Enabled: profile.Enabled,
		Match: apiTypes.ExtractorProfileMatch{
			CommandRegex: profile.Match.CommandRegex,
			ArgsRegex:    profile.Match.ArgsRegex,
		},
		Rules: rules,
	}
}

func ruleDefinitionFromAPI(rule apiTypes.ExtractorRule) pty.RuleDefinition {
	return pty.RuleDefinition{
		ID:      rule.ID,
		Enabled: rule.Enabled,
		Trigger: pty.RuleTrigger{RegionChanged: regionTriggerFromAPI(rule.Trigger.RegionChanged)},
		Extract: pty.RuleExtract{
			Type:    rule.Extract.Type,
			Region:  regionFromAPI(rule.Extract.Region),
			Pattern: rule.Extract.Pattern,
		},
		Emit: pty.RuleEmit{
			Kind:         rule.Emit.Kind,
			UpdateWindow: rule.Emit.UpdateWindow,
			Finalize:     rule.Emit.Finalize,
			Open:         rule.Emit.Open,
		},
		Identity: identityFromAPI(rule.Identity),
	}
}

func ruleDefinitionToAPI(rule pty.RuleDefinition) apiTypes.ExtractorRule {
	return apiTypes.ExtractorRule{
		ID:      rule.ID,
		Enabled: rule.Enabled,
		Trigger: apiTypes.ExtractorTrigger{RegionChanged: regionTriggerToAPI(rule.Trigger.RegionChanged)},
		Extract: apiTypes.ExtractorExtract{
			Type:    rule.Extract.Type,
			Region:  regionToAPI(rule.Extract.Region),
			Pattern: rule.Extract.Pattern,
		},
		Emit: apiTypes.ExtractorEmit{
			Kind:         rule.Emit.Kind,
			UpdateWindow: rule.Emit.UpdateWindow,
			Finalize:     rule.Emit.Finalize,
			Open:         rule.Emit.Open,
		},
		Identity: identityToAPI(rule.Identity),
	}
}

func regionTriggerFromAPI(trigger *apiTypes.ExtractorRegionTrigger) *pty.RegionTrigger {
	if trigger == nil {
		return nil
	}
	return &pty.RegionTrigger{
		Top:    trigger.Top,
		Bottom: trigger.Bottom,
		Left:   trigger.Left,
		Right:  trigger.Right,
	}
}

func regionTriggerToAPI(trigger *pty.RegionTrigger) *apiTypes.ExtractorRegionTrigger {
	if trigger == nil {
		return nil
	}
	return &apiTypes.ExtractorRegionTrigger{
		Top:    trigger.Top,
		Bottom: trigger.Bottom,
		Left:   trigger.Left,
		Right:  trigger.Right,
	}
}

func regionFromAPI(region apiTypes.ExtractorRegion) pty.RegionSpec {
	return pty.RegionSpec{
		Top:    region.Top,
		Bottom: region.Bottom,
		Left:   region.Left,
		Right:  region.Right,
	}
}

func regionToAPI(region pty.RegionSpec) apiTypes.ExtractorRegion {
	return apiTypes.ExtractorRegion{
		Top:    region.Top,
		Bottom: region.Bottom,
		Left:   region.Left,
		Right:  region.Right,
	}
}

func identityFromAPI(identity *apiTypes.ExtractorIdentity) *pty.RuleIdentity {
	if identity == nil {
		return nil
	}
	return &pty.RuleIdentity{Capture: identity.Capture, Static: identity.Static}
}

func identityToAPI(identity *pty.RuleIdentity) *apiTypes.ExtractorIdentity {
	if identity == nil {
		return nil
	}
	return &apiTypes.ExtractorIdentity{Capture: identity.Capture, Static: identity.Static}
}

func toAPIActivityRecord(record pty.ActivityRecord) apiTypes.ExtractorActivityRecord {
	resp := apiTypes.ExtractorActivityRecord{
		Type: record.Type,
		ID:   record.ID,
		Rev:  record.Rev,
		TS:   record.TS,
	}
	if record.Entry != nil {
		resp.Entry = &apiTypes.ExtractorActivityEntry{
			ID:        record.Entry.ID,
			SessionID: record.Entry.SessionID,
			Kind:      record.Entry.Kind,
			TS:        record.Entry.TS,
			Rev:       record.Entry.Rev,
			Open:      record.Entry.Open,
			Data:      record.Entry.Data,
		}
	}
	return resp
}

func findRuleProfile(cfg *pty.RuleConfig, id string) (pty.RuleProfile, error) {
	for _, profile := range cfg.Profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return pty.RuleProfile{}, os.ErrNotExist
}
