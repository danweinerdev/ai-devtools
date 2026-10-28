// OpenCode 2 plugin entry. OpenCode resolves the plugin from this file and
// reads `id` and `setup(ctx)` from the default export; setup registers the
// aid-* agents and skills, routes models and bridges guards to the `aid`
// binary (.opencode-plugin/src/setup.js).
//
// Keep the export surface to the default export alone: a module without an
// object default export falls back to OpenCode's legacy loader, which rejects
// any export that isn't a function. The implementation modules export helpers
// for their tests, so OpenCode must never be pointed at them directly.
import { PLUGIN_ID, setup } from "./.opencode-plugin/src/setup.js"

export default {
  id: PLUGIN_ID,
  setup,
}
