import mark from "../assets/svolo-mark.svg";
/** Two paths, one direction: the human and the agent share the same workspace. */
export function SvoloMark({ className = "" }: { className?: string }) {
  return <span className={`inline-flex items-center gap-2 ${className}`} role="img" aria-label="Svolo">
    <img src={mark} alt="" draggable={false} className="h-[1.3em] w-[1.3em]" />
    <span aria-hidden="true" className="font-semibold tracking-tight">Svolo</span>
  </span>;
}
