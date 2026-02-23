import { createMemo, createSignal, Show } from "solid-js";
import type { FileReadResponse } from "../api/files";

interface ImageViewerProps {
  file: FileReadResponse;
  isTreeOpen?: boolean;
  onToggleTree?: () => void;
}

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / 1024).toFixed(1) + " KB";
}

export default function ImageViewer(props: ImageViewerProps) {
  const [dimensions, setDimensions] = createSignal<{ w: number; h: number } | null>(null);

  const filename = createMemo(() => {
    const parts = props.file.path.split("/");
    return parts[parts.length - 1];
  });

  const dataUri = createMemo(
    () => `data:${props.file.mime_type};base64,${props.file.content}`,
  );

  const approxBytes = createMemo(() => props.file.content.length * 0.75);

  function handleLoad(e: Event) {
    const img = e.target as HTMLImageElement;
    setDimensions({ w: img.naturalWidth, h: img.naturalHeight });
  }

  return (
    <div class="image-viewer-wrap">
      <div class="editor-toolbar">
        <button
          type="button"
          class="editor-tree-toggle"
          aria-label={props.isTreeOpen ? "Hide files" : "Browse files"}
          aria-expanded={props.isTreeOpen ?? false}
          onClick={() => props.onToggleTree?.()}
        >
          <span aria-hidden="true">☰</span>
        </button>
        <span class="image-viewer-path">{props.file.path}</span>
        <span class="image-viewer-meta">
          {filename()} &middot; {formatBytes(approxBytes())} &middot; {props.file.mime_type}
          <Show when={dimensions()}>
            {(d) => <> &middot; {d().w} &times; {d().h}</>}
          </Show>
        </span>
      </div>
      <div class="image-viewer-canvas">
        <img
          class="image-viewer-img"
          src={dataUri()}
          alt={filename()}
          onLoad={handleLoad}
        />
      </div>
    </div>
  );
}
