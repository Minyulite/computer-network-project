// A few line icons from lucide (ISC license), inlined so the app needs no icon package.
// One size and stroke everywhere; they take the color of the surrounding text.
import Link from "next/link";
import type { ReactNode } from "react";

function Icon({ children, size = 16 }: { children: ReactNode; size?: number }) {
  return <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden focusable="false">{children}</svg>;
}

export const SearchIcon = () => <Icon><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></Icon>;
export const PlusIcon = () => <Icon><path d="M5 12h14" /><path d="M12 5v14" /></Icon>;
export const CheckIcon = () => <Icon><path d="M20 6 9 17l-5-5" /></Icon>;
export const ArrowLeftIcon = () => <Icon><path d="m12 19-7-7 7-7" /><path d="M19 12H5" /></Icon>;
export const EyeIcon = () => <Icon>
  <path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0" />
  <circle cx="12" cy="12" r="3" />
</Icon>;
export const EyeOffIcon = () => <Icon>
  <path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49" />
  <path d="M14.084 14.158a3 3 0 0 1-4.242-4.242" />
  <path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143" />
  <path d="m2 2 20 20" />
</Icon>;
export const SpellCheckIcon = () => <Icon><path d="m6 16 6-12 6 12" /><path d="M8 12h8" /><path d="m16 20 2 2 4-4" /></Icon>;
export const StoreIcon = () => <Icon>
  <path d="m2 7 4.41-4.41A2 2 0 0 1 7.83 2h8.34a2 2 0 0 1 1.42.59L22 7" />
  <path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8" />
  <path d="M15 22v-4a2 2 0 0 0-2-2h-2a2 2 0 0 0-2 2v4" />
  <path d="M2 7h20" />
  <path d="M22 7v3a2 2 0 0 1-2 2 2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 16 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 12 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 8 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 4 12a2 2 0 0 1-2-2V7" />
</Icon>;

/** The product mark: the arrow from the original logo, drawn flat in the accent. */
export function Brand({ href }: { href?: string }) {
  const content = <><svg className="brand-mark" width={18} height={18} viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden focusable="false"><path d="M7 17 17 7" /><path d="M8 7h9v9" /></svg>Commonroom</>;
  return href ? <Link className="brand" href={href}>{content}</Link> : <span className="brand">{content}</span>;
}
