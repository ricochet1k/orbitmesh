/**
 * Card & Panel Component Demo & Test
 * 
 * Demonstrates all card variants, panel styles, and layouts
 * using the design system tokens.
 */

export function CardDemo() {
  return (
    <div style={{ padding: 'var(--space-6)', backgroundColor: 'var(--color-surface-1)' }}>
      <h2 style={{ marginBottom: 'var(--space-4)', fontSize: 'var(--heading-h2-size)' }}>
        Card & Panel Component System
      </h2>

      {/* Basic Cards */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Card Variants
        </h3>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
            gap: 'var(--space-4)',
          }}
        >
          {/* Default Card */}
          <div className="card">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Default Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              Standard card with default elevation and styling.
            </p>
          </div>

          {/* Simple Card */}
          <div className="card card-simple">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Simple Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              Minimal styling with subtle border only.
            </p>
          </div>

          {/* Elevated Card */}
          <div className="card card-elevated">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Elevated Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              Enhanced shadow for more prominence.
            </p>
          </div>

          {/* Outlined Card */}
          <div className="card card-outlined">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Outlined Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              Border only, no background fill.
            </p>
          </div>

          {/* Filled Card */}
          <div className="card card-filled">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Filled Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              Subtle colored background surface.
            </p>
          </div>

          {/* Ghost Card */}
          <div className="card card-ghost">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Ghost Card
            </h4>
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              No visible container styling.
            </p>
          </div>
        </div>
      </section>

      {/* Status Cards */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Status/Semantic Cards
        </h3>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
            gap: 'var(--space-4)',
          }}
        >
          {/* Success Card */}
          <div className="card card-success">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Success
            </h4>
            <p style={{ fontSize: 'var(--body-text-size)' }}>
              Operation completed successfully.
            </p>
          </div>

          {/* Warning Card */}
          <div className="card card-warning">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Warning
            </h4>
            <p style={{ fontSize: 'var(--body-text-size)' }}>
              Please review before proceeding.
            </p>
          </div>

          {/* Error Card */}
          <div className="card card-error">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Error
            </h4>
            <p style={{ fontSize: 'var(--body-text-size)' }}>
              Something went wrong. Try again.
            </p>
          </div>

          {/* Info Card */}
          <div className="card card-info">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Info
            </h4>
            <p style={{ fontSize: 'var(--body-text-size)' }}>
              Additional information about this action.
            </p>
          </div>
        </div>
      </section>

      {/* Card with Sections */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Structured Card
        </h3>
        <div className="card" style={{ maxWidth: '500px' }}>
          <div className="card-header">
            <h4 style={{ margin: 0, fontSize: 'var(--heading-h4-size)' }}>Card Title</h4>
            <div className="card-header-actions">
              <button className="btn btn-ghost btn-sm">Action</button>
            </div>
          </div>
          <div className="card-body">
            <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
              This card demonstrates the header, body, and footer sections.
            </p>
          </div>
          <div className="card-footer">
            <span style={{ fontSize: 'var(--font-size-xs)', color: 'var(--color-text-tertiary)' }}>
              Created today
            </span>
            <div className="card-footer-actions">
              <button className="btn btn-secondary btn-sm">Cancel</button>
              <button className="btn btn-primary btn-sm">Save</button>
            </div>
          </div>
        </div>
      </section>

      {/* Panel Example */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Panel Component
        </h3>
        <div className="panel" style={{ maxWidth: '600px' }}>
          <div className="panel-header">
            <div className="panel-header-title">
              <h2 style={{ margin: 0 }}>Panel Title</h2>
              <p className="panel-header-subtitle">
                Supporting description for this panel
              </p>
            </div>
            <div className="panel-header-actions">
              <button className="btn btn-secondary btn-sm">Settings</button>
            </div>
          </div>

          <div className="panel-content">
            <p style={{ color: 'var(--color-text-secondary)' }}>
              Panel content goes here. The panel component is designed for larger sections of
              content and provides more structured layout options than cards.
            </p>

            <div style={{ marginTop: 'var(--space-4)' }}>
              <h4>Features</h4>
              <ul style={{ marginTop: 'var(--space-2)', paddingLeft: 'var(--space-4)' }}>
                <li>Header with title and actions</li>
                <li>Main content area</li>
                <li>Footer for additional actions</li>
              </ul>
            </div>
          </div>

          <div className="panel-footer">
            <span className="panel-footer-meta">Last updated: Today</span>
            <div className="panel-footer-actions">
              <button className="btn btn-secondary btn-sm">Cancel</button>
              <button className="btn btn-primary btn-sm">Apply</button>
            </div>
          </div>
        </div>
      </section>

      {/* Card Grid */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Card Grid Layout
        </h3>
        <div className="card-grid">
          {[1, 2, 3, 4, 5, 6].map((item) => (
            <div key={item} className="card">
              <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
                Card {item}
              </h4>
              <p style={{ color: 'var(--color-text-secondary)', fontSize: 'var(--body-text-size)' }}>
                Cards in a responsive grid layout automatically adjust to screen size.
              </p>
            </div>
          ))}
        </div>
      </section>

      {/* Animation Demo */}
      <section>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Animated Cards
        </h3>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
            gap: 'var(--space-4)',
          }}
        >
          <div className="card animate-fade-in">
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Fade In
            </h4>
            <p>This card fades in on page load.</p>
          </div>
          <div className="card animate-slide-up" style={{ animationDelay: '100ms' }}>
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Slide Up
            </h4>
            <p>This card slides up with a delay.</p>
          </div>
          <div className="card animate-scale-in" style={{ animationDelay: '200ms' }}>
            <h4 style={{ marginBottom: 'var(--space-2)', fontSize: 'var(--heading-h4-size)' }}>
              Scale In
            </h4>
            <p>This card scales in from center.</p>
          </div>
        </div>
      </section>
    </div>
  );
}
