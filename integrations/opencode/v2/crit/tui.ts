import { Plugin } from "@opencode/plugin/tui"
import { CritRPC } from "./lib/crit-rpc.js"

export default Plugin.define({
  id: "crit-tui",
  setup(context) {
    const crit = context.client.rpc(CritRPC)
    return crit.events.on("waitStarted", () => {
      context.ui.toast.show({
        title: "Crit",
        message: "Crit is waiting for your review. Finish it in your browser when ready.",
        variant: "info",
      })
    })
  },
})
