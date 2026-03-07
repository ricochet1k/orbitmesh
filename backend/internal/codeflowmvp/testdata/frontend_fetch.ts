export async function loadSession(id: string): Promise<Response> {
  return fetch(`/api/sessions/${id}`, {
    method: "GET",
  })
}

export async function createSession(body: unknown): Promise<Response> {
  return window.fetch("/api/sessions", {
    method: "POST",
    body: JSON.stringify(body),
  })
}
