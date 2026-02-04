# Monitoring & Metrics Design

## Overview
OrbitMesh requires comprehensive monitoring to track agent performance, resource consumption, and system health. This document outlines the metrics data model, collection strategy, and visualization requirements.

## Metrics Data Model

### Agent Metrics
- `tokens_in`: Number of prompt tokens sent to the LLM.
- `tokens_out`: Number of completion tokens received from the LLM.
- `request_count`: Total number of API requests made to the provider.
- `latency_ms`: Time taken for provider operations (Start, RunPrompt).
- `success_rate`: Ratio of successful vs. failed operations.

### System Metrics
- `active_sessions`: Number of currently running agent sessions.
- `cpu_usage`: CPU utilization of the backend process and its children.
- `memory_usage`: Memory consumption of the backend process.
- `goroutine_count`: Number of active goroutines in the backend.

### Task/Commit Metrics
- `tasks_completed`: Number of tasks successfully finished.
- `lines_changed`: Total lines of code modified by agents over time.
- `merge_conflicts`: Number of conflicts encountered and resolved.

## Collection Strategy

### Backend (Go)
- **Instrumentation**: Use `internal/metrics` to expose counters and gauges.
- **Provider Hooks**: Providers will emit `MetricEvent`s containing token and latency data.
- **Sampling**: System metrics will be collected every 15-30 seconds.
- **Persistence**: Short-term metrics will be kept in memory; historical data will be persisted using the existing JSON storage model.

### Frontend (SolidJS)
- **SSE Stream**: The dashboard will subscribe to the `/events` endpoint to receive real-time metric updates.
- **Aggregation**: The frontend will aggregate metrics for display in charts and status badges.

## Visualization Requirements

- **Real-time Dashboard**: Live gauges for token usage and active sessions.
- **Historical Charts**: Line charts showing token consumption and LOC changes over time.
- **Resource Monitor**: CPU/Memory utilization graphs for the orchestration system.

## Storage Strategy
Historical metrics will be stored in a dedicated directory:
`~/.orbitmesh/metrics/<session-id>/<timestamp>.json`
This ensures metrics are persistent across restarts without requiring an external database.
