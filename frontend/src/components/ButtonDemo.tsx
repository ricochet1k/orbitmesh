/**
 * Button Component Demo & Test
 * 
 * Demonstrates all button variants, sizes, and states
 * using the design system tokens.
 */

export function ButtonDemo() {
  return (
    <div style={{ padding: 'var(--space-6)', backgroundColor: 'var(--color-surface-1)' }}>
      <h2 style={{ marginBottom: 'var(--space-4)', fontSize: 'var(--heading-h2-size)' }}>
        Button Component System
      </h2>

      {/* Primary Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Primary Buttons
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-4)', flexWrap: 'wrap' }}>
          <button className="btn btn-primary btn-sm">Small Primary</button>
          <button className="btn btn-primary btn-md">Medium Primary</button>
          <button className="btn btn-primary btn-lg">Large Primary</button>
          <button className="btn btn-primary btn-md" disabled>
            Disabled Primary
          </button>
        </div>
      </section>

      {/* Secondary Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Secondary Buttons
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-4)', flexWrap: 'wrap' }}>
          <button className="btn btn-secondary btn-sm">Small Secondary</button>
          <button className="btn btn-secondary btn-md">Medium Secondary</button>
          <button className="btn btn-secondary btn-lg">Large Secondary</button>
          <button className="btn btn-secondary btn-md" disabled>
            Disabled Secondary
          </button>
        </div>
      </section>

      {/* Ghost Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Ghost Buttons (Tertiary)
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-4)', flexWrap: 'wrap' }}>
          <button className="btn btn-ghost btn-sm">Small Ghost</button>
          <button className="btn btn-ghost btn-md">Medium Ghost</button>
          <button className="btn btn-ghost btn-lg">Large Ghost</button>
          <button className="btn btn-ghost btn-md" disabled>
            Disabled Ghost
          </button>
        </div>
      </section>

      {/* Danger Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Danger Buttons (Destructive)
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-4)', flexWrap: 'wrap' }}>
          <button className="btn btn-danger btn-sm">Delete (Small)</button>
          <button className="btn btn-danger btn-md">Remove</button>
          <button className="btn btn-danger btn-lg">Confirm Delete</button>
          <button className="btn btn-danger btn-md" disabled>
            Disabled Danger
          </button>
        </div>
      </section>

      {/* Success Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Success Buttons
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-4)', flexWrap: 'wrap' }}>
          <button className="btn btn-success btn-sm">Approve (Small)</button>
          <button className="btn btn-success btn-md">Confirm</button>
          <button className="btn btn-success btn-lg">Complete</button>
        </div>
      </section>

      {/* Button Groups */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Button Groups
        </h3>
        <div className="btn-group">
          <button className="btn btn-primary btn-md">Save</button>
          <button className="btn btn-secondary btn-md">Cancel</button>
          <button className="btn btn-danger btn-md">Delete</button>
        </div>
      </section>

      {/* Toggle Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Toggle Buttons
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
          <button className="btn btn-toggle active btn-md">Active</button>
          <button className="btn btn-toggle btn-md">Inactive</button>
          <button className="btn btn-toggle btn-md">Off</button>
        </div>
      </section>

      {/* Icon Buttons */}
      <section style={{ marginBottom: 'var(--space-8)' }}>
        <h3 style={{ marginBottom: 'var(--space-3)', fontSize: 'var(--heading-h3-size)' }}>
          Icon Buttons
        </h3>
        <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
          <button className="btn btn-icon btn-primary" title="Add">
            ✓
          </button>
          <button className="btn btn-icon btn-secondary" title="Settings">
            ⚙
          </button>
          <button className="btn btn-icon btn-danger" title="Delete">
            ✕
          </button>
        </div>
      </section>
    </div>
  );
}
