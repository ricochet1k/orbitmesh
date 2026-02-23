import { createFileRoute } from "@tanstack/solid-router"
import FilesPage from "../views/FilesPage"

type FilesSearch = { path?: string }

export const Route = createFileRoute('/files')({
  validateSearch: (s) => s as FilesSearch,
  component: FilesPage,
})
