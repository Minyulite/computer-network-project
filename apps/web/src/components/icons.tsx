// A few line icons from lucide (ISC license), inlined so the app needs no icon package.
// One size and stroke everywhere; they take the color of the surrounding text.
import Link from "next/link";
import type { ReactNode } from "react";

type Props = { size?: number };

function Icon({ children, size = 16 }: { children: ReactNode; size?: number }) {
  return <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden focusable="false">{children}</svg>;
}

export const SearchIcon = ({ size }: Props) => <Icon size={size}><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></Icon>;
export const PlusIcon = ({ size }: Props) => <Icon size={size}><path d="M5 12h14" /><path d="M12 5v14" /></Icon>;
export const CheckIcon = () => <Icon><path d="M20 6 9 17l-5-5" /></Icon>;
export const ArrowLeftIcon = ({ size }: Props) => <Icon size={size}><path d="m12 19-7-7 7-7" /><path d="M19 12H5" /></Icon>;
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
export const StoreIcon = ({ size }: Props) => <Icon size={size}>
  <path d="m2 7 4.41-4.41A2 2 0 0 1 7.83 2h8.34a2 2 0 0 1 1.42.59L22 7" />
  <path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8" />
  <path d="M15 22v-4a2 2 0 0 0-2-2h-2a2 2 0 0 0-2 2v4" />
  <path d="M2 7h20" />
  <path d="M22 7v3a2 2 0 0 1-2 2 2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 16 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 12 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 8 12a2.7 2.7 0 0 1-1.59-.63.7.7 0 0 0-.82 0A2.7 2.7 0 0 1 4 12a2 2 0 0 1-2-2V7" />
</Icon>;

export const ChatIcon = ({ size }: Props) => <Icon size={size}><path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z" /></Icon>;
export const UserIcon = ({ size }: Props) => <Icon size={size}><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" /><circle cx="12" cy="7" r="4" /></Icon>;
export const UsersIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" /><circle cx="9" cy="7" r="4" />
  <path d="M22 21v-2a4 4 0 0 0-3-3.87" /><path d="M16 3.13a4 4 0 0 1 0 7.75" />
</Icon>;
export const LogOutIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5" /><path d="M21 12H9" />
</Icon>;
export const SendIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z" />
  <path d="m21.854 2.147-10.94 10.939" />
</Icon>;
export const SlidersIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M21 4h-7" /><path d="M10 4H3" /><path d="M21 12h-9" /><path d="M8 12H3" /><path d="M21 20h-5" /><path d="M12 20H3" />
  <path d="M14 2v4" /><path d="M8 10v4" /><path d="M16 18v4" />
</Icon>;

export const BotIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M12 8V4H8" /><rect width="16" height="12" x="4" y="8" rx="2" />
  <path d="M2 14h2" /><path d="M20 14h2" /><path d="M15 13v2" /><path d="M9 13v2" />
</Icon>;
export const UserPlusIcon = ({ size }: Props) => <Icon size={size}>
  <path d="M2 21a8 8 0 0 1 13.292-6" /><circle cx="10" cy="8" r="5" /><path d="M19 16v6" /><path d="M22 19h-6" />
</Icon>;
export const LanguagesIcon = ({ size }: Props) => <Icon size={size}>
  <path d="m5 8 6 6" /><path d="m4 14 6-6 2-3" /><path d="M2 5h12" /><path d="M7 2h1" /><path d="m22 22-5-10-5 10" /><path d="M14 18h6" />
</Icon>;

/** The product mark on its own, for the narrow rail. */
export const BrandMark = ({ size = 24 }: Props) => <svg className="brand-mark" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
  strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden focusable="false"><path d="M7 17 17 7" /><path d="M8 7h9v9" /></svg>;

/** The product mark: the arrow from the original logo, drawn flat in the accent. */
export function Spinner({ size = 14, label }: { size?: number; label?: string }) {
  return <span className="spinner" style={{ width: size, height: size }} role={label ? "status" : undefined} aria-label={label} aria-hidden={label ? undefined : true} />;
}

export function Brand({ href }: { href?: string }) {
  const content = <><svg className="brand-mark" width={18} height={18} viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden focusable="false"><path d="M7 17 17 7" /><path d="M8 7h9v9" /></svg>Commonroom</>;
  return href ? <Link className="brand" href={href}>{content}</Link> : <span className="brand">{content}</span>;
}
