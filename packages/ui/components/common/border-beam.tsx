/**
 * Decorative highlight that sweeps around the host's rounded border. Render it
 * as a child of a positioned (`relative`), rounded host; the ring inherits the
 * host's border radius.
 */
export function BorderBeam() {
  return (
    <span className="border-beam-ring" aria-hidden="true">
      <span className="border-beam-light" />
    </span>
  );
}
