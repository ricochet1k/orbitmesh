import { JSX } from 'solid-js'

export interface SkeletonLoaderProps {
  variant?: 'text' | 'card' | 'list' | 'table' | 'graph'
  count?: number
  height?: string
  width?: string
  className?: string
}

export default function SkeletonLoader(props: SkeletonLoaderProps): JSX.Element {
  const renderSkeleton = () => {
    switch (props.variant) {
      case 'text':
        return (
          <div class="skeleton-text-container">
            {Array.from({ length: props.count || 3 }).map((_, i) => (
              <div 
                class="skeleton skeleton-text animate-shimmer" 
                style={{ 
                  width: i === (props.count || 3) - 1 ? '70%' : '100%',
                  height: props.height || '1em'
                }} 
              />
            ))}
          </div>
        )
      
      case 'card':
        return (
          <div class="skeleton-card-container">
            {Array.from({ length: props.count || 1 }).map(() => (
              <div class="skeleton-card">
                <div class="skeleton skeleton-card-header animate-shimmer" />
                <div class="skeleton skeleton-card-body animate-shimmer" />
                <div class="skeleton skeleton-card-footer animate-shimmer" />
              </div>
            ))}
          </div>
        )
      
      case 'list':
        return (
          <div class="skeleton-list-container">
            {Array.from({ length: props.count || 5 }).map(() => (
              <div class="skeleton-list-item">
                <div class="skeleton skeleton-list-icon animate-shimmer" />
                <div class="skeleton-list-content">
                  <div class="skeleton skeleton-list-title animate-shimmer" />
                  <div class="skeleton skeleton-list-subtitle animate-shimmer" />
                </div>
                <div class="skeleton skeleton-list-badge animate-shimmer" />
              </div>
            ))}
          </div>
        )
      
      case 'table':
        return (
          <div class="skeleton-table-container">
            <div class="skeleton-table-header">
              {Array.from({ length: 5 }).map(() => (
                <div class="skeleton skeleton-table-header-cell animate-shimmer" />
              ))}
            </div>
            {Array.from({ length: props.count || 5 }).map(() => (
              <div class="skeleton-table-row">
                {Array.from({ length: 5 }).map(() => (
                  <div class="skeleton skeleton-table-cell animate-shimmer" />
                ))}
              </div>
            ))}
          </div>
        )
      
      case 'graph':
        return (
          <div class="skeleton-graph-container">
            <div class="skeleton skeleton-graph animate-shimmer" style={{ height: props.height || '360px' }} />
            <div class="skeleton-graph-label">Loading system graph...</div>
          </div>
        )
      
      default:
        return (
          <div 
            class={`skeleton animate-shimmer ${props.className || ''}`}
            style={{
              width: props.width || '100%',
              height: props.height || '20px'
            }}
          />
        )
    }
  }

  return <>{renderSkeleton()}</>
}
