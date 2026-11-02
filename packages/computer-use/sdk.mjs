/** App objects carry identity; all authority stays in the device broker. */
export function createComputer(call) {
  return Object.freeze({
    async getApp(app) {
      if (typeof app !== "string" || !app.trim()) throw new Error("Supply an application name or bundle ID");
      const target = await call({ op: "get_app", app });
      let observation;
      const run = async (op, input = {}) => call({ ...input, op, app: target.id });
      const observed = (input) => {
        if (!observation) throw new Error("Observe the application before targeting a control or coordinate");
        return { ...input, observationId: observation.observationId };
      };
      const action = async (op, input) => {
        const args = observed(input);
        observation = undefined;
        return run(op, args);
      };
      return Object.freeze({
        id: target.id, name: target.name,
        windows: () => run("windows"),
        async selectWindow(windowId) { observation = undefined; return run("select_window", { windowId }); },
        async observe() { observation = await run("observe"); return observation; },
        click: (target) => action("click", typeof target === "string" ? { ref: target } : target),
        scroll: (input) => action("scroll", input),
        drag: (input) => action("drag", input),
        setValue: (ref, value) => action("set_value", { ref, value }),
        typeText: (text) => action("type", { text }),
        pressKey: (key) => action("press", { key }),
      });
    },
  });
}
