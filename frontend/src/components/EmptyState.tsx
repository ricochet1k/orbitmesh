import { JSX } from 'solid-js'

export interface EmptyStateProps {
  icon?: string
  title: string
  description: string
  action?: {
    label: string
    onClick: () => void
  }
  secondaryAction?: {
    label: string
    onClick: () => void
  }
  variant?: 'default' | 'info' | 'warning'
}

export default function EmptyState(props: EmptyStateProps): JSX.Element {
  const variantClass = () => {
    switch (props.variant) {
      case 'info': return 'empty-state-info'
      case 'warning': return 'empty-state-warning'
      default: return 'empty-state-default'
    }
  }

  return (
    <div class={`empty-state-container ${variantClass()}`}>
      {props.icon && <div class="empty-state-icon" role="presentation">{props.icon}</div>}
      <h3 class="empty-state-title">{props.title}</h3>
      <p class="empty-state-description">{props.description}</p>
      {props.action && (
        <div class="empty-state-actions">
          <button 
            type="button" 
            class="empty-state-action-primary" 
            onClick={props.action.onClick}
          >
            {props.action.label}
          </button>
          {props.secondaryAction && (
            <button 
              type="button" 
              class="empty-state-action-secondary" 
              onClick={props.secondaryAction.onClick}
            >
              {props.secondaryAction.label}
            </button>
          )}
        </div>
      )}
    </div>
  )
}
