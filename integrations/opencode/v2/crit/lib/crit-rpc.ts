import { Rpc } from "@opencode/plugin/rpc"

export const CritRPC = Rpc.define({
  id: "crit",
  methods: {},
  events: {
    waitStarted: {
      schema: {
        type: "object",
        properties: {},
        additionalProperties: false,
      },
    },
  },
})
