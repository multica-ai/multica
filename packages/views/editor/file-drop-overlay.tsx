function FileDropOverlay() {
  return (
    <div className="pointer-events-none absolute inset-0 z-10 rounded-[inherit] border border-dashed border-brand/20 bg-brand/[0.02] animate-in fade-in duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:animate-none" />
  );
}

export { FileDropOverlay };
