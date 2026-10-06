/** Shared product glyph. Status colors express state, never provider identity. */
export const STATE_COLORS = { waiting: "#e5c07b", failed: "#ff8f8f", unread: "#60d9b0" };
const upper = "M 11 126 C 27 79 47 64 88 51 L 207 12 C 193 53 171 74 132 88 Z";
const lower = "M 56 175 C 79 130 97 112 132 99 L 195 73 C 180 117 151 141 111 154 Z";
type Props = { size?: number; color?: string; className?: string };
export function SvoloGlyph({ size = 64, color = "currentColor", className = "" }: Props) {
  return <svg viewBox="0 0 256 256" width={size} height={size} className={className} role="img" aria-label="Svolo">
    <g transform="translate(0 33)" fill={color}><path d={upper}/><path d={lower}/><circle cx="231" cy="43" r="13"/></g>
  </svg>;
}
export function SvoloSpinner({ size = 14, className = "" }: { size?: number; className?: string }) {
  return <span className={`svolo-loading shrink-0 ${className}`} aria-hidden="true"><SvoloGlyph size={size} color="var(--accent)"/></span>;
}
