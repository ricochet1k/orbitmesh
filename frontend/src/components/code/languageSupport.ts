import { javascript } from "@codemirror/lang-javascript"
import { go } from "@codemirror/lang-go"
import { css } from "@codemirror/lang-css"
import { html } from "@codemirror/lang-html"
import { json } from "@codemirror/lang-json"
import { python } from "@codemirror/lang-python"
import { rust } from "@codemirror/lang-rust"
import { markdown } from "@codemirror/lang-markdown"
import type { LanguageSupport } from "@codemirror/language"

export function getLanguageSupportForFile(mimeType: string, path: string): LanguageSupport | null {
  const ext = path.includes(".") ? "." + path.split(".").pop()! : ""

  if (ext === ".ts" || ext === ".tsx" || mimeType === "application/typescript") {
    return javascript({ typescript: true, jsx: path.endsWith(".tsx") })
  }
  if (
    ext === ".js" ||
    ext === ".jsx" ||
    mimeType === "text/javascript" ||
    mimeType === "application/javascript"
  ) {
    return javascript({ jsx: path.endsWith(".jsx") })
  }
  if (ext === ".go" || mimeType === "text/x-go") {
    return go()
  }
  if (ext === ".css" || mimeType === "text/css") {
    return css()
  }
  if (ext === ".html" || mimeType === "text/html") {
    return html()
  }
  if (ext === ".json" || mimeType === "application/json") {
    return json()
  }
  if (ext === ".py" || mimeType === "text/x-python") {
    return python()
  }
  if (ext === ".rs" || mimeType === "text/x-rust") {
    return rust()
  }
  if (ext === ".md" || mimeType === "text/markdown") {
    return markdown()
  }

  return null
}

export function getLanguageSupportForFence(languageName: string | undefined): LanguageSupport | null {
  const normalized = (languageName ?? "").trim().toLowerCase()
  if (!normalized) return null

  switch (normalized) {
    case "ts":
    case "typescript":
      return javascript({ typescript: true })
    case "tsx":
      return javascript({ typescript: true, jsx: true })
    case "js":
    case "javascript":
      return javascript()
    case "jsx":
      return javascript({ jsx: true })
    case "go":
      return go()
    case "css":
      return css()
    case "html":
      return html()
    case "json":
      return json()
    case "py":
    case "python":
      return python()
    case "rs":
    case "rust":
      return rust()
    case "md":
    case "markdown":
      return markdown()
    default:
      return null
  }
}
