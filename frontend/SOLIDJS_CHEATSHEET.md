# SolidJS Cheatsheet for OrbitMesh

SolidJS is **not React**. Components run **once**. Reactivity is tracked at the
signal-read level, not the component level. Forgetting this is the root cause of
almost every SolidJS bug.

---

## Core Mental Model

| Concept | React | SolidJS |
|---|---|---|
| Component function | Re-runs on every render | Runs **once**, never again |
| Reactive value | `useState` — triggers re-render | `createSignal` — surgically updates DOM |
| Derived value | `useMemo` | `createMemo` (or a plain `() =>` function) |
| Side effect | `useEffect` | `createEffect` |
| DOM ref | `useRef` | Plain `let ref!: HTMLElement` assigned via `ref={el => ref = el}` or `ref={ref}` |
| Context | `React.createContext` + `useContext` | `createContext` + `useContext`, but prefer module-level signal singletons for global state |
| List rendering | `array.map(...)` in JSX | `<For each={...}>` — **never `.map()` in JSX** |
| Conditional rendering | `&&` / ternary in JSX | `<Show when={...}>` / `<Switch>` + `<Match>` |

---

## Signals

```tsx
const [count, setCount] = createSignal(0)

// READ — always call the signal as a function
count()           // reactive read (tracked)

// WRITE
setCount(5)
setCount(prev => prev + 1)   // updater form
```

**Rules:**
- Reading `count` (without `()`) gives you a function reference — never reactive, never the value.
- Signal reads inside JSX, `createEffect`, `createMemo`, and `createResource` sources are automatically tracked.
- Signal reads inside plain event handlers or `setTimeout` callbacks are **not** tracked — that is intentional and fine.

---

## Derived Values

### Plain arrow function (cheap derivation, no caching)

```tsx
const isRunning = () => session()?.state === "running"
const sessionId = () => props.sessionId ?? localId() ?? ""
```

Use this for simple compositions. It is reactive wherever it is called in a reactive context (JSX, `createEffect`, another `createMemo`).

### `createMemo` (cached, runs only when dependencies change)

```tsx
const filteredList = createMemo(() => {
  return items().filter(item => item.matches(query()))
})
```

Use `createMemo` when the derivation is expensive or when you need a stable reference. It short-circuits if the result is the same value (`===`).

---

## Effects

```tsx
createEffect(() => {
  const id = sessionId()      // tracked dependency
  if (!id) return

  const handler = (e: Event) => doSomething(e)
  document.addEventListener("click", handler)

  // Cleanup runs before the effect re-runs or when the owner is disposed
  onCleanup(() => document.removeEventListener("click", handler))
})
```

**Rules:**
- `createEffect` tracks all signal reads that happen **synchronously** during its execution.
- Return value is ignored — use `onCleanup` for teardown.
- Never create signals or stores inside `createEffect` — create them at the component level.
- `createEffect` does **not** run during SSR by default (use `createRenderEffect` for that).

### `onMount` / `onCleanup`

```tsx
onMount(() => {
  // Runs once after the component's DOM is inserted
  fetchInitialData()
})

onCleanup(() => {
  // Runs when the component is removed from the DOM
  stream.close()
})
```

`onMount` is sugar for `createEffect` that runs once. It does **not** re-run.

---

## Async Data: `createResource`

```tsx
// No source (runs once, re-run with refetch())
const [user, { refetch }] = createResource(() => apiClient.getUser())

// With reactive source (re-runs when source changes, skips when source is falsy/undefined)
const [session] = createResource(
  () => sessionId(),                         // source: falsy = skip fetch
  (id) => apiClient.getSession(id),          // fetcher
)

// Derived source object (common pattern to pass multiple values)
const source = createMemo(() => {
  const id = sessionId()
  const cursor = page()
  if (!id) return undefined
  return { id, cursor }
})
const [page] = createResource(source, ({ id, cursor }) => apiClient.getPage(id, cursor))
```

**States:** `resource.loading`, `resource.error`, `resource()` (the value).

**In JSX use `<Show>`:**
```tsx
<Show when={!session.loading} fallback={<Spinner />}>
  <Show when={session()} fallback={<Empty />}>
    {(s) => <SessionView session={s()} />}
  </Show>
</Show>
```

---

## Stores (mutable nested state)

```tsx
import { createStore } from "solid-js/store"

const [form, setForm] = createStore({ name: "", active: false })

// Fine-grained path update — only the `name` field subscribers re-run
setForm("name", e.currentTarget.value)
setForm("active", true)

// Nested path
setForm("config", "timeout", 30)

// Updater function
setForm("tags", tags => [...tags, "new"])
```

**Never mutate the store object directly** — always use the setter.

---

## Control Flow (JSX)

### Lists — use `<For>`, never `.map()`

```tsx
// CORRECT — fine-grained DOM updates
<For each={items()}>
  {(item) => <div>{item.name}</div>}
</For>

// WRONG — static snapshot, does not react to array changes
{items().map(item => <div>{item.name}</div>)}
```

`<For>` keys by object identity. Use `<Index>` when the index matters more than identity (e.g., primitive arrays).

### Conditionals

```tsx
<Show when={isLoggedIn()} fallback={<LoginPage />}>
  <Dashboard />
</Show>

// Keyed Show — re-mounts child when value changes (not just truthy/falsy)
<Show when={selectedItem()} keyed>
  {(item) => <Detail item={item} />}
</Show>

<Switch fallback={<NotFound />}>
  <Match when={route() === "home"}><Home /></Match>
  <Match when={route() === "settings"}><Settings /></Match>
</Switch>
```

---

## Props

### Reactive props — never destructure at the top level

```tsx
// WRONG — loses reactivity, value is captured once
const { name, onChange } = props

// CORRECT — use props.name directly
<div>{props.name}</div>
```

### `mergeProps` for default prop values

```tsx
// CORRECT — mergeProps is for merging/defaulting props, not for safe destructuring
const props = mergeProps({ size: "md", disabled: false }, rawProps)
```

### Typing reactive props with `Accessor<T>`

When a prop is meant to be a reactive value (a signal getter), type it explicitly:

```tsx
interface TableProps {
  sessions: Accessor<SessionResponse[]>
  isLoading: Accessor<boolean>
  onSelect: (id: string) => void
}
```

This signals to callers that they should pass `sessions={list}` where `list` is `() => T`, not the raw value.

### `splitProps` for spreading extras

```tsx
const [local, rest] = splitProps(props, ["class", "onClick"])
return <button class={local.class} onClick={local.onClick} {...rest} />
```

---

## Global State (module-level singletons)

Prefer module-level signals over React Context for global state. Signals defined at
module scope are fine — no wrapper needed:

```ts
// state/project.ts
const [projects, setProjects] = createSignal<Project[]>([])
const [active, setActive] = createSignal<string | null>(null)

export { projects, active, setActive }
```

Use `createRoot` only if you need the ability to explicitly **dispose** the reactive
computation at some point (e.g., in tests or dynamically loaded modules). For
app-lifetime state it adds no benefit.

```ts
// Only use createRoot when you need the dispose handle
const dispose = createRoot((dispose) => {
  const [x, setX] = createSignal(0)
  // ... setup effects ...
  return dispose
})
// Later: dispose() to tear everything down
```

---

## `untrack` — opt out of tracking

Use `untrack` to read a signal without registering it as a dependency of the
surrounding reactive context:

```tsx
// Read without subscribing — the surrounding effect won't re-run when someSignal changes
const initialValue = untrack(() => someSignal())

createEffect(() => {
  const id = sessionId()           // tracked — effect re-runs when sessionId changes
  const cur = untrack(() => page()) // NOT tracked — just reading the current value
  doSomething(id, cur)
})
```

## `batch` — defer effect notifications

Use `batch` when you need multiple signal writes to be treated as a single update,
so that downstream effects and memos only re-run once:

```tsx
import { batch } from "solid-js"

batch(() => {
  setMessages([])
  setPage(0)
  setInitialized(false)
  // effects fire once after all three writes, not three times
})
```

---

## Hooks in This Codebase

"Hooks" here are plain functions (no special rules, no call-order restrictions). They can use reactive primitives freely as long as they are called inside an active reactive owner (a component, `createRoot`, or `createEffect`).

```ts
// hooks/useSessionStream.ts — registers onCleanup on the calling owner
export function useSessionStream(url: string, handlers: Handlers): void {
  const stream = startEventStream(url)
  onCleanup(() => stream.close())  // belongs to whichever owner called this function
}
```

When calling such a hook inside a `createEffect`, the `onCleanup` is registered to that effect's scope and fires before the effect re-runs or when the component unmounts. This is correct and expected behavior.

---

## Common Mistakes

### 1. Calling a signal without `()`

```tsx
// WRONG — passes the function reference to JSX, not the value
<div>{count}</div>

// CORRECT
<div>{count()}</div>
```

### 2. Destructuring props

```tsx
// WRONG — name is captured once, not reactive
const MyComp = ({ name }: Props) => <div>{name}</div>

// CORRECT
const MyComp = (props: Props) => <div>{props.name}</div>
```

### 3. `.map()` in JSX

```tsx
// WRONG — static, does not update when the array changes
{list().map(x => <Item x={x} />)}

// CORRECT
<For each={list()}>{(x) => <Item x={x} />}</For>
```

### 4. `&&` for conditional rendering with non-boolean left side

```tsx
// WRONG — renders "0" when count is 0 (same as React, but doubly confusing in SolidJS)
{count() && <Badge count={count()} />}

// CORRECT
<Show when={count() > 0}><Badge count={count()} /></Show>
```

### 5. Creating reactive state inside `createEffect`

```tsx
// WRONG — creates a new signal every time the effect re-runs
createEffect(() => {
  const [x, setX] = createSignal(0)   // BUG
})

// CORRECT — create signals at component scope
const [x, setX] = createSignal(0)
createEffect(() => { /* use x() here */ })
```

### 6. Reading a resource value directly without checking loading state

```tsx
// Can throw or return undefined before data is available
<div>{user().name}</div>

// CORRECT
<Show when={user()}>{(u) => <div>{u().name}</div>}</Show>
```

### 7. `className` prop naming

SolidJS uses `class`, not `className`, both in JSX and in prop interfaces:

```tsx
// WRONG
<div className={props.className} />

// CORRECT
<div class={props.class} />
```

---

## Quick Reference

| Task | API |
|---|---|
| Local state | `createSignal` |
| Derived (cheap) | `() => expr` |
| Derived (expensive/cached) | `createMemo` |
| Side effect | `createEffect` |
| Async fetch | `createResource` |
| Nested/complex state | `createStore` (solid-js/store) |
| Run once after mount | `onMount` |
| Teardown | `onCleanup` |
| Skip tracking | `untrack` |
| Batch multiple writes | `batch` |
| List rendering | `<For>` / `<Index>` |
| Conditional rendering | `<Show>` / `<Switch>` + `<Match>` |
| Default prop values | `mergeProps` |
| Spread remaining props | `splitProps` |
| Global state | module-level signals |
| Disposable reactive root | `createRoot` |
| Prop typing for reactivity | `Accessor<T>` (i.e., `() => T`) |
