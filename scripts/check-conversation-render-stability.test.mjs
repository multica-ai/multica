import assert from "node:assert/strict";
import test from "node:test";
import { checkSource } from "./check-conversation-render-stability.mjs";

const codes = (source) =>
  checkSource(source).map((diagnostic) => diagnostic.code);

test("requires observers for functions, forwardRef, memo and classes", () => {
  for (const source of [
    "function Row() { return <div />; }",
    "const Row = memo(function Row() { return <div />; });",
    "const Row = forwardRef((props, ref) => <div ref={ref} />);",
    "class Row extends Component { render() { return <div />; } }"
  ])
    assert.deepEqual(codes(source), ["observer"]);
});

test("accepts explicit observers and preserves custom memo comparators", () => {
  for (const source of [
    "function RowRender() { return <div />; } const Row = observer(RowRender);",
    "const Row = memo(observer(function Row() { return <div />; }), compare);",
    "const Row = observer(forwardRef((props, ref) => <div ref={ref} />));",
    "class Row extends Component { render() { return <div />; } } observeClass(Row);"
  ])
    assert.deepEqual(codes(source), []);
});

test("rejects inline props including conditional allocations and list closures", () => {
  for (const expression of [
    "{}",
    "[]",
    "() => select()",
    "enabled ? {} : undefined",
    "enabled && (() => select())"
  ]) {
    assert.ok(
      codes(
        `const Row = observer(() => <Button value={${expression}} />);`
      ).includes("stable-props")
    );
  }
  assert.ok(
    codes(
      "const Rows = observer(() => items.map(item => <Button onClick={() => select(item.id)} />));"
    ).includes("stable-props")
  );
});

test("checks named render allocations and forwarded useCallback handlers", () => {
  assert.deepEqual(
    codes(
      "const Row = observer(function Row() { const style = {}; return <div style={style} />; });"
    ),
    ["stable-props"]
  );
  assert.deepEqual(
    codes(
      "const Row = observer(function Row() { const onClick = useCallback(click, [value]); return <Button onClick={onClick} />; });"
    ),
    ["persist-callback"]
  );
});

test("accepts memoized data, persistent callbacks and module constants", () => {
  assert.deepEqual(
    codes(
      "const style = {}; const Row = observer(function Row() { const labels = useMemo(() => ({}), [locale]); const click = usePersistCallback(() => select(id)); return <Button style={style} labels={labels} onClick={click} />; });"
    ),
    []
  );
});

test("checks each component's own binding when names are reused", () => {
  const source = `
    const First = observer(function First() { const style = {}; return <div style={style} />; });
    const Second = observer(function Second() { const style = useMemo(() => ({}), []); return <div style={style} />; });
  `;
  assert.deepEqual(codes(source), ["stable-props"]);
});

test("rejects array projections and bound callbacks at prop boundaries", () => {
  for (const expression of [
    "rows.map(row => row.id)",
    "rows.filter(Boolean)",
    "click.bind(null, id)",
    "executionTargets(workspace)"
  ]) {
    assert.ok(
      codes(
        `const Row = observer(() => <List value={${expression}} />);`
      ).includes("stable-props")
    );
  }
  assert.deepEqual(
    codes(
      "const Row = observer(() => <div title={rows.map(format).join(', ')} />);"
    ),
    []
  );
});

test("rejects wrapping an observer or memo component a second time", () => {
  assert.ok(
    codes("const Row = observer(observer(() => <div />));").includes(
      "double-observer"
    )
  );
  assert.ok(
    codes("const Row = observer(memo(() => <div />));").includes(
      "double-observer"
    )
  );
});

test("checks effect-only components and createElement providers", () => {
  assert.ok(
    codes(
      "function Probe(): null { useEffect(watch, []); return null; }"
    ).includes("observer")
  );
  assert.ok(
    checkSource(
      "export function Provider({ value, children }: Props) { return React.createElement(Context.Provider, { value }, children); }",
      "provider.ts"
    ).some((diagnostic) => diagnostic.code === "observer")
  );
});
