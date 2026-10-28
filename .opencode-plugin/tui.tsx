// Shows the conversation's active sub-agents and the model each is running on.
// OpenCode compiles this TSX at runtime; local directory plugins load <dir>/tui
// (the repo root's tui.tsx re-exports this). Plugins can't annotate OpenCode's
// built-in sub-agent cards, so the sidebar carries the information instead.
import { Plugin } from "@opencode/plugin/tui"
import { createMemo, For, Show } from "solid-js"
import { readActiveSubagents, reuseResult } from "./src/active-subagents.js"

export default Plugin.define({
  id: "ai-development.tui",
  setup(context) {
    const ActiveSubagents = (props: { readonly sessionID: string }) => {
      // Solid passes the memo's previous value in; reusing unchanged rows keeps
      // <For> from recreating rows that did not change.
      const result = createMemo((previous?: ReturnType<typeof readActiveSubagents>) =>
        reuseResult(previous, readActiveSubagents(props.sessionID, {
          root: (id: string) => context.data.session.root(id),
          family: (id: string) => context.data.session.family(id),
          get: (id: string) => context.data.session.get(id),
          status: (id: string) => context.data.session.status(id),
        })),
      )
      const rows = () => result().rows
      const error = () => result().error

      // A read failure is shown, never folded into "none": an empty list must
      // only ever mean that nothing is running.
      return (
        <box flexDirection="column">
          <text fg={context.theme.text.base}>Active sub-agents</text>
          <Show
            when={rows().length > 0}
            fallback={
              <text fg={context.theme.text.faint}>{error() ? `unavailable: ${error()}` : "none"}</text>
            }
          >
            <For each={rows()}>
              {(row) => (
                <box flexDirection="row">
                  <text fg={context.theme.text.base}>{`${row.agent}  `}</text>
                  <text fg={context.theme.text.muted}>{row.model}</text>
                </box>
              )}
            </For>
            <Show when={error()}>
              <text fg={context.theme.text.faint}>{`incomplete: ${error()}`}</text>
            </Show>
          </Show>
        </box>
      )
    }

    return context.ui.slot({
      append: "sidebar.content",
      render: (props) => <ActiveSubagents sessionID={props.sessionID} />,
    })
  },
})
