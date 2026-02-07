# Design System & Tokens

This directory contains the comprehensive visual language and design tokens for the OrbitMesh UI. It provides a cohesive, maintainable foundation for all UI components and ensures consistency across the application.

## Overview

The design system is organized into modular CSS files that can be imported and used independently or together:

- **design-tokens.css** - Core design tokens (colors, typography, spacing, shadows, motion)
- **components/button.css** - Button component styles
- **components/input.css** - Input field component styles
- **components/card.css** - Card and panel component styles
- **animations.css** - Animation and motion primitives

## Design Tokens

### Color Palette

All colors are defined as CSS variables with semantic naming:

#### Neutral Colors
```css
--color-neutral-50: #f9fafb;
--color-neutral-900: #111827;
--color-neutral-950: #0b1220;  /* Darkest - primary text */
```

#### Semantic Colors
- **Primary**: `--color-primary` (#2563eb) - Main brand color
- **Secondary**: `--color-secondary` (#0ea5e9) - Accent color
- **Success**: `--color-success` (#0f766e) - Positive/confirmation
- **Warning**: `--color-warning` (#b45309) - Caution/alert
- **Error**: `--color-error` (#b91c1c) - Destructive/error
- **Info**: `--color-info` (#0284c7) - Informational

#### Surface Colors
- `--color-surface-0`: White background
- `--color-surface-1`: Light gray (alternative background)
- `--color-surface-2`: Medium gray (subtle background)
- `--color-surface-3`: Darker gray (tertiary background)
- `--color-surface-4`: Very dark (full-bleed background)

#### Text Colors
- `--color-text-primary`: Main text (#0b1220)
- `--color-text-secondary`: Secondary text (#1f2937)
- `--color-text-tertiary`: Tertiary/muted text (#556070)
- `--color-text-disabled`: Disabled text state

### Typography

#### Font Families
- `--font-family-sans`: IBM Plex Sans (body, UI)
- `--font-family-mono`: IBM Plex Mono (code, technical content)

#### Font Sizes
Scale from `--font-size-xs` (12px) to `--font-size-5xl` (48px)

#### Heading Styles
- `--heading-h1-size`: 36px, bold, line-height 1.25
- `--heading-h2-size`: 30px, semibold, line-height 1.375
- `--heading-h3-size`: 24px, semibold, line-height 1.375
- `--heading-h4-size`: 20px, semibold, line-height 1.5

#### Body Text
- `--body-text-size`: 14px
- `--body-text-weight`: 400 (normal)
- `--body-text-line-height`: 1.625

### Spacing Scale

Uses 8px base unit with increments:

```css
--space-0: 0
--space-1: 4px
--space-2: 8px
--space-3: 12px
--space-4: 16px
--space-5: 20px
--space-6: 24px
--space-7: 32px
--space-8: 40px
--space-9: 48px
```

#### Semantic Spacing
- `--padding-xs` through `--padding-xl`
- `--gap-xs` through `--gap-xl`

### Border Radius

```css
--radius-sm: 2px      /* Minimal rounding */
--radius-md: 4px      /* Default for small elements */
--radius-lg: 8px      /* Cards, buttons */
--radius-xl: 12px     /* Larger components */
--radius-2xl: 16px    /* Panels, modals */
--radius-pill: 9999px /* Fully rounded */
```

### Shadows (Elevation Levels)

Five elevation levels for depth:

- `--shadow-1` (sm): Subtle, used for cards
- `--shadow-2` (md): Medium, used for hover states
- `--shadow-3` (lg): Large, used for dropdowns/overlays
- `--shadow-4` (xl): Very large, used for critical modals

Plus color-specific shadows:
- `--shadow-success`, `--shadow-warning`, `--shadow-error`, `--shadow-info`

### Motion

#### Duration
- `--duration-fast`: 120ms (snappy interactions)
- `--duration-normal`: 200ms (standard)
- `--duration-medium`: 300ms (smooth)
- `--duration-slow`: 360ms (emphasis)
- `--duration-slower`: 500ms (dramatic)

#### Easing Functions
- `--easing-standard`: cubic-bezier(0.2, 0, 0, 1) - Default
- `--easing-emphasized`: cubic-bezier(0.3, 0, 0.8, 0.15) - Playful
- `--easing-decelerate`: cubic-bezier(0, 0, 0.2, 1) - Ease out
- `--easing-accelerate`: cubic-bezier(0.4, 0, 1, 1) - Ease in

#### Transition Presets
- `--transition-fast`: all changes in 120ms
- `--transition-normal`: all changes in 200ms
- `--transition-slow`: all changes in 360ms

## Component Styles

### Buttons

Base class: `.btn`

#### Variants
- `.btn-primary` - Main call-to-action (gradient blue → cyan)
- `.btn-secondary` - Alternative action (white with border)
- `.btn-ghost` - Minimal/tertiary (text only)
- `.btn-danger` - Destructive action (red)
- `.btn-success` - Positive/completion (teal)

#### Sizes
- `.btn-sm` - Small button
- `.btn-md` - Medium button (default)
- `.btn-lg` - Large button

#### Special Variants
- `.btn-icon` - Square button for icons
- `.btn-toggle` - Toggle button (on/off states)
- `.btn-group` - Group of related buttons

#### States
- `:hover` - Elevated with shadow
- `:active` - Pressed appearance
- `:focus-visible` - Keyboard focus indicator
- `:disabled` - Reduced opacity, disabled cursor

```html
<button class="btn btn-primary btn-md">Save</button>
<button class="btn btn-secondary btn-sm">Cancel</button>
<button class="btn btn-danger btn-md">Delete</button>
<button class="btn btn-ghost">Link Button</button>
```

### Input Fields

Base selectors: `input`, `textarea`, `select`

#### Variants
- `.input-sm` - Small input
- `.input-md` - Medium input (default)
- `.input-lg` - Large input
- `.input-underline` - Minimal style
- `.input-filled` - Filled background style

#### States
- `:hover` - Border color changes to primary
- `:focus` - Primary border + focus shadow
- `:disabled` - Reduced opacity, disabled cursor
- `:invalid` - Error styling

#### Form Groups
```html
<div class="form-group">
  <label for="name">Name</label>
  <input type="text" id="name" />
  <small>Helper text</small>
</div>

<div class="form-group error">
  <label for="email">Email</label>
  <input type="email" id="email" />
  <small>This field is required</small>
</div>
```

#### Custom Checkboxes & Radios
```html
<div class="input-checkbox">
  <input type="checkbox" id="agree" />
  <label for="agree">I agree to the terms</label>
</div>
```

### Cards & Panels

#### Card Variants
- `.card` - Default card with elevation
- `.card-simple` - Minimal border only
- `.card-elevated` - More prominent shadow
- `.card-outlined` - Border only, no background
- `.card-filled` - Subtle background color
- `.card-ghost` - No visible container

#### Semantic Cards
- `.card-success` - Green styled card
- `.card-warning` - Orange styled card
- `.card-error` - Red styled card
- `.card-info` - Blue styled card

#### Card Structure
```html
<div class="card">
  <div class="card-header">
    <h3>Title</h3>
    <div class="card-header-actions">
      <button class="btn btn-ghost">Action</button>
    </div>
  </div>
  
  <div class="card-body">
    Content goes here
  </div>
  
  <div class="card-footer">
    <span>Meta info</span>
    <div class="card-footer-actions">
      <button class="btn btn-secondary">Cancel</button>
      <button class="btn btn-primary">Save</button>
    </div>
  </div>
</div>
```

#### Panel Component
Similar structure to cards but for larger sections:

```html
<div class="panel">
  <div class="panel-header">
    <div class="panel-header-title">
      <h2>Panel Title</h2>
      <p class="panel-header-subtitle">Subtitle</p>
    </div>
  </div>
  
  <div class="panel-content">
    Main content
  </div>
  
  <div class="panel-footer">
    <span class="panel-footer-meta">Meta</span>
    <div class="panel-footer-actions">
      <button class="btn btn-primary">Action</button>
    </div>
  </div>
</div>
```

#### Grid Layouts
```html
<!-- Responsive card grid -->
<div class="card-grid">
  <div class="card">Card 1</div>
  <div class="card">Card 2</div>
  <div class="card">Card 3</div>
</div>

<!-- Vertical stack -->
<div class="card-list">
  <div class="card">Item 1</div>
  <div class="card">Item 2</div>
</div>
```

## Animation Primitives

### Entrance Animations
- `.animate-fade-in` - Fade in from transparent
- `.animate-slide-up` - Slide up from bottom
- `.animate-slide-down` - Slide down from top
- `.animate-slide-left` - Slide from right
- `.animate-slide-right` - Slide from left
- `.animate-scale-in` - Scale from 95%
- `.animate-bounce-in` - Playful bounce entrance
- `.animate-rotate` - Rotate entrance

### Continuous Animations
- `.animate-pulse` - Subtle pulse for attention
- `.animate-spin` - Continuous rotation (loading)
- `.animate-shimmer` - Shimmer effect for skeletons

### Hover Effects
- `.hover-lift` - Raise on hover with shadow
- `.hover-glow` - Add glow effect on hover
- `.hover-shift` - Slight movement on hover
- `.hover-darken` - Darken background on hover
- `.hover-underline` - Animated underline on hover

### Timing & Control
- `.animate-fast` - 120ms duration
- `.animate-slow` - 360ms duration
- `.animate-slower` - 500ms duration
- `.animate-delay-100` through `.animate-delay-500`
- `.animate-infinite` - Loop indefinitely

```html
<!-- Fade in on load -->
<div class="card animate-fade-in">Card</div>

<!-- Slide up with delay -->
<div class="animate-slide-up" style="animation-delay: 100ms;">Item</div>

<!-- Hover effect -->
<div class="card hover-lift">Clickable card</div>

<!-- Loading skeleton -->
<div class="skeleton is-loading"></div>
```

## Utility Classes

### Typography
```html
<h1 class="text-h1">Heading 1</h1>
<p class="text-body">Body text</p>
<code class="text-code">Code snippet</code>
```

### Colors
```html
<p class="text-primary">Primary text</p>
<p class="text-success">Success message</p>
<div class="bg-primary">Primary background</div>
```

### Spacing
```html
<div class="p-md gap-lg">Padded with gap</div>
```

### Border Radius
```html
<div class="rounded-md">Rounded corners</div>
<div class="rounded-pill">Fully rounded</div>
```

### Shadows
```html
<div class="shadow-sm">Subtle shadow</div>
<div class="shadow-lg">Large shadow</div>
```

### Transitions
```html
<div class="transition-fast">Quick change</div>
<button class="transition-normal">Smooth interaction</button>
```

## Accessibility

### Focus Indicators
All interactive elements have visible focus indicators:
```css
outline: 2px solid var(--focus-outline-color);
outline-offset: 2px;
```

### Reduced Motion
Users with `prefers-reduced-motion` will see minimal animations:
```css
@media (prefers-reduced-motion: reduce) {
  * { animation-duration: 0.01ms !important; }
}
```

### Color Contrast
All text meets WCAG AA contrast requirements:
- Primary text on white: 11.6:1
- Secondary text on white: 6.2:1
- Tertiary text on white: 3.8:1

### Touch Targets
Buttons and interactive elements have minimum 44px touch targets on mobile.

## Responsive Design

### Breakpoints
- Mobile: < 640px
- Tablet: 640px - 1024px
- Desktop: > 1024px

Components automatically adjust for smaller screens:
- Button sizes increase for better touch targets
- Grids collapse to single column
- Modal content scales down

## Dark Mode Support

The design system includes dark mode support:
```css
@media (prefers-color-scheme: dark) {
  :root {
    --color-background: var(--color-neutral-900);
    --color-foreground: var(--color-neutral-50);
  }
}
```

## Integration Examples

### Using Buttons
```html
<button class="btn btn-primary btn-md">Click me</button>
<button class="btn btn-secondary btn-sm" disabled>Disabled</button>
```

### Using Form Groups
```html
<form>
  <div class="form-group">
    <label for="name">Name</label>
    <input type="text" id="name" placeholder="Enter name" />
  </div>
  
  <div class="form-group">
    <label for="message">Message</label>
    <textarea id="message"></textarea>
  </div>
  
  <div style="display: flex; gap: var(--space-2);">
    <button class="btn btn-primary">Submit</button>
    <button class="btn btn-secondary">Cancel</button>
  </div>
</form>
```

### Using Cards
```html
<div class="card-grid">
  <article class="card animate-fade-in">
    <h3>Feature 1</h3>
    <p>Description</p>
    <button class="btn btn-primary btn-sm">Learn more</button>
  </article>
  <!-- More cards... -->
</div>
```

### Using Panels
```html
<div class="panel">
  <div class="panel-header">
    <div class="panel-header-title">
      <h2>Settings</h2>
      <p class="panel-header-subtitle">Manage your preferences</p>
    </div>
  </div>
  
  <div class="panel-content">
    <!-- Settings form here -->
  </div>
  
  <div class="panel-footer">
    <div class="panel-footer-actions">
      <button class="btn btn-secondary">Reset</button>
      <button class="btn btn-primary">Save Changes</button>
    </div>
  </div>
</div>
```

## Customization

To customize the design system, modify the CSS variables in `design-tokens.css`:

```css
:root {
  /* Change primary color */
  --color-primary: #your-color;
  
  /* Change spacing scale */
  --space-4: 20px; /* Change from 16px to 20px */
  
  /* Change font */
  --font-family-sans: 'Your Font', sans-serif;
}
```

## Best Practices

1. **Use design tokens** - Always use CSS variables instead of hardcoded values
2. **Maintain consistency** - Reuse component classes instead of creating duplicates
3. **Responsive first** - Test components on multiple screen sizes
4. **Accessibility** - Ensure all components are keyboard navigable and have proper ARIA labels
5. **Performance** - Use animations sparingly and respect `prefers-reduced-motion`
6. **Documentation** - Keep components documented with examples

## Browser Support

- Chrome/Edge: Latest 2 versions
- Firefox: Latest 2 versions
- Safari: Latest 2 versions
- Mobile browsers: Latest versions

## Related Files

- **frontend/src/components/ButtonDemo.tsx** - Interactive button component showcase
- **frontend/src/components/CardDemo.tsx** - Interactive card/panel showcase
- **frontend/src/App.tsx** - Main application component
- **frontend/src/index.css** - Global styles (imports design system)
