import Image from "next/image";
import type { User } from "@/lib/api";
import { avatarSrc, personAvatar } from "@/lib/avatars";
import { BotIcon, UsersIcon } from "@/components/icons";

type Size = "sm" | "md" | "lg";
const pixels: Record<Size, number> = { sm: 32, md: 44, lg: 56 };

// The files are small static SVGs, so they skip Next's image optimizer.
function Picture({ id, size }: { id: string; size: Size }) {
  return <Image src={avatarSrc(id)} alt="" width={pixels[size]} height={pixels[size]} unoptimized draggable={false} />;
}

type Props = { id: string; size?: Size; online?: boolean; bot?: boolean; ghost?: boolean };

// A cartoon picture. A dot on its corner shows presence: green when `online` is true, grey when false,
// none when it is left out. The badge marks a shop assistant.
// Pictures are decoration: the name next to them is what a screen reader hears.
export function Avatar({ id, size = "md", online, bot, ghost }: Props) {
  return <span className={`avatar ${size}${ghost ? " ghost" : ""}`} aria-hidden>
    <Picture id={id} size={size} />
    {online !== undefined && <i className={online ? "presence" : "presence idle"} />}
    {bot && <span className="bot-badge"><BotIcon size={12} /></span>}
  </span>;
}

export function PersonAvatar({ user, ...rest }: { user: User } & Omit<Props, "id">) {
  return <Avatar id={personAvatar(user)} {...rest} />;
}

// Two members' pictures overlapping, like a group on a social network.
export function GroupAvatar({ members, size = "md" }: { members: User[]; size?: Size }) {
  const [first, second] = members;
  return <span className={`avatar group-stack ${size}`} aria-hidden>
    <span className="stack-a">{first ? <Picture id={personAvatar(first)} size={size} /> : <UsersIcon size={16} />}</span>
    <span className="stack-b">{second ? <Picture id={personAvatar(second)} size={size} /> : <UsersIcon size={14} />}</span>
  </span>;
}

// A native radio group, so arrow keys and the form's own data work with no extra code.
export function AvatarPicker({ ids, name, label, value, onChange }: { ids: string[]; name: string; label: string; value: string; onChange: (id: string) => void }) {
  return <fieldset className="avatar-picker"><legend>{label}</legend>
    <div className="avatar-grid">
      {ids.map((id, index) => <label className="avatar-choice" key={id}>
        <input type="radio" name={name} value={id} checked={value === id} onChange={() => onChange(id)} />
        <span className="sr-only">Avatar {index + 1}</span>
        <Avatar id={id} size="lg" />
      </label>)}
    </div>
  </fieldset>;
}
