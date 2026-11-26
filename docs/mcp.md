# FileForge MCP server

FileForge ships a local [Model Context Protocol](https://modelcontextprotocol.io)
server. An MCP client such as Claude Desktop, Cursor or OpenCode can discover the
FileForge toolbox, plan a job, and run it — locally, with no cloud service and no
account.

The server is `fileforge-cli mcp serve`. It speaks MCP over stdio, so the MCP
client starts it as a child process. There is no port, no daemon and no token.

## What it can and cannot do

**Six tools, nothing else:**

| Tool | What it does |
| --- | --- |
| `list_tools` | The FileForge catalog: id, name, description, input and output extensions, batch support, accepted option keys |
| `plan_job` | Validate a job and return the planned output paths and warnings. Writes nothing |
| `run_job` | Plan and run a job, returning status, outputs and a canonical error |
| `list_recipes` | The recipe catalog |
| `plan_recipe` | Validate a recipe and return its planned outputs. Runs no step |
| `run_recipe` | Plan and run a recipe |

Deliberately **not** exposed: automation rules, file organizing, duplicate
scanning, or anything else that moves or deletes files you already have. An agent
can convert files through FileForge; it cannot reorganize your folders.

## Security model

Access is a folder allowlist, stored at
`<user config dir>/fileforge/mcp.json` (on Linux `~/.config/fileforge/mcp.json`,
on Windows `%APPDATA%\fileforge\mcp.json`).

- **An empty allowlist denies everything.** There is no default to the home
  directory or the working directory.
- **Only the terminal can grant.** There is no MCP tool that adds, removes or
  lists permissions. If there were, an agent could ask for `~` and the consent
  model would be theatre.
- **Every path is resolved.** Inputs, output directories and path-valued options
  are resolved through symlinks and must land inside an allowed root. `..` is
  resolved before comparison, and a symlink pointing outside is refused.
- **Nothing destructive is reachable**, even if a future tool of that kind is
  registered: `run_job` only accepts the conversion domains.
- **Every run is audited** to `mcp_audit.log` as one JSON line:

  ```json
  {"time":"2026-11-26T15:46:34Z","tool":"tool.pdf.merge","inputCount":2,
   "outputDir":"/home/you/Documents/Exports","status":"success"}
  ```

  `status` is `success`, `denied` (path outside the allowlist, with
  `errorCode`) or `rejected` (failed validation before running). File contents
  and option values are never recorded.

## Setup

1. Put `fileforge-cli` somewhere your client can execute it:
   - Windows installer: `%ProgramFiles%\FileForge\FileForge Desktop\fileforge-cli.exe`
   - Linux deb/rpm/Arch: `/usr/local/bin/fileforge-cli`
   - AppImage: the CLI ships beside the `.AppImage` file; move it onto your `PATH`
2. Grant at least one folder:

   ```bash
   fileforge-cli mcp allow ~/Documents/Exports
   fileforge-cli mcp list
   ```

3. Point your MCP client at `fileforge-cli mcp serve` (below).
4. Restart the client after changing the allowlist. Grants are read when the
   server starts.

## Client configuration

The transport is plain stdio, so any MCP client works. The shapes below are the
common ones; check your client's documentation for the exact file location.

**Claude Desktop** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "fileforge": {
      "command": "/home/you/.local/bin/fileforge-cli",
      "args": ["mcp", "serve"]
    }
  }
}
```

**Cursor** (`.cursor/mcp.json` in the project, or the global equivalent):

```json
{
  "mcpServers": {
    "fileforge": {
      "command": "/home/you/.local/bin/fileforge-cli",
      "args": ["mcp", "serve"]
    }
  }
}
```

**OpenCode** (`opencode.json`):

```json
{
  "mcp": {
    "servers": {
      "fileforge": {
        "type": "local",
        "command": ["/home/you/.local/bin/fileforge-cli", "mcp", "serve"]
      }
    }
  }
}
```

On Windows, use the full path with backslashes or forward slashes and the `.exe`
extension.

## Tool arguments

`plan_job` and `run_job`:

| Argument | Required | Notes |
| --- | --- | --- |
| `toolId` | yes | For example `tool.pdf.merge` |
| `inputPaths` | yes | Absolute paths, 1 to 50 per call, each inside an allowed root |
| `outputDir` | no | Must also be inside an allowed root; defaults to the input folder |
| `options` | no | Tool options; keys the tool does not accept are rejected with `TOOL_OPTION_UNKNOWN` |

`plan_recipe` and `run_recipe` take `recipeId` instead of `toolId`, with the same
`inputPaths`, `outputDir` and `options`.

Every tool and recipe error carries the canonical code the desktop app uses
(`VALIDATION_INVALID_INPUT`, `RUNTIME_DEP_MISSING`, `EXEC_TIMEOUT_TRANSIENT`, …),
so a client can branch on the code instead of parsing messages.

## A worked session

```
> allow a folder
$ fileforge-cli mcp allow ~/Documents/Exports
allowed /home/you/Documents/Exports

> what can it do
→ list_tools
← [ { "id": "tool.pdf.merge", "name": "PDF Merge", "batch": false, ... }, ... ]

> plan first
→ plan_job { "toolId": "tool.pdf.merge",
             "inputPaths": ["/home/you/Documents/Exports/a.pdf",
                            "/home/you/Documents/Exports/b.pdf"],
             "outputDir": "/home/you/Documents/Exports" }
← { "success": true, "plannedOutputs": [
      { "outputPath": "/home/you/Documents/Exports/a_merged.pdf" } ] }

> then run
→ run_job { ...same arguments... }
← { "success": true, "status": "success",
    "outputs": [ { "path": "/home/you/Documents/Exports/a_merged.pdf",
                   "sizeBytes": 4792 } ] }
```

Plan before run is the intended flow: the plan shows the exact destination and
any warnings without writing a byte, and `run_job` re-plans internally before
executing, so a wrong path never becomes a half-written file.

## Limitations

- **No grants over MCP.** Change the allowlist from the terminal and restart the
  client.
- **Grants are read at startup**, not watched. A running server keeps the list it
  loaded.
- **Calls are synchronous and sequential.** A long job blocks the next call, and
  there are no MCP progress notifications or cancel tool. Use the desktop app for
  long interactive work.
- **50 inputs per call** is a hard cap, enforced in the schema and in validation.
- **The server is the CLI**, so it needs the same runtime as the CLI. On Windows
  the binary is ~21 MB because it links libvips for the image tools.
- **The audit log grows without rotation.** It is one line per `run_*` call;
  delete it when it gets large.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `MCP_PATH_NOT_ALLOWED` | The path is outside every allowed root. Run `fileforge-cli mcp allow <dir>` and restart the client |
| `no allowed roots are configured` | The allowlist is empty. Nothing is allowed by default |
| The client lists no tools | The command path is wrong, or the client is not spawning a stdio server |
| Protocol errors after a first successful call | Something is writing to stdout. Only JSON-RPC may go there; FileForge logs to stderr |
| A granted folder still denied | The path resolves elsewhere after symlinks, or the server has not been restarted |

## Related

- [CLI usage](../README.md#release-artifacts) — the same binary without MCP.
- [Release process](release.md) — how the CLI reaches the artifacts.
