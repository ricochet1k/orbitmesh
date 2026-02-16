import { JSX } from 'solid-js'
import { Link } from '@tanstack/solid-router'

interface EmptyStateAction {
  label: string
  onClick?: () => void
  href?: string
  target?: string
  rel?: string
}

export interface EmptyStateProps {
  icon?: string
  title: string
  description: string
  action?: EmptyStateAction
  secondaryAction?: EmptyStateAction
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

  const renderAction = (action: EmptyStateAction, className: string) => {
    if (action.href) {
      const rel = action.rel ?? (action.target === "_blank" ? "noreferrer" : undefined)
      return (
        <Link to={action.href} class={className} target={action.target} rel={rel}>
          {action.label}
        </Link>
      )
    }
    return (
      <button type="button" class={className} onClick={action.onClick}>
        {action.label}
      </button>
    )
  }

  return (
    <div class={`empty-state-container ${variantClass()}`}>
      {props.icon && <div class="empty-state-icon" role="presentation">{props.icon}</div>}
      <h3 class="empty-state-title">{props.title}</h3>
      <p class="empty-state-description">{props.description}</p>
      {props.action && (
        <div class="empty-state-actions">
          {renderAction(props.action, "empty-state-action-primary")}
          {props.secondaryAction && (
            renderAction(props.secondaryAction, "empty-state-action-secondary")
          )}
        </div>
      )}
    </div>
  )
}
