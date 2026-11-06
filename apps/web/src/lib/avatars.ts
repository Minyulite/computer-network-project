// Cartoon pictures live in public/avatars: p01 to p12 for people, b01 to b08 for shop assistants.
// The server validates the identifiers; this file only knows how to show them.
const ids = (prefix: string, count: number) => Array.from({ length: count }, (_, i) => `${prefix}${String(i + 1).padStart(2, "0")}`);

export const personAvatars = ids("p", 12);
export const botAvatars = ids("b", 8);

export const avatarSrc = (id: string) => `/avatars/${id}.svg`;
export const randomOf = (list: string[]) => list[Math.floor(Math.random() * list.length)];

// An account made before avatars existed has none; pick one from its ID so it
// always looks the same, on every device.
export function personAvatar(user: { id: string; avatar?: string }) {
  if (user.avatar && personAvatars.includes(user.avatar)) return user.avatar;
  let hash = 0;
  for (const char of user.id) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return personAvatars[hash % personAvatars.length];
}

export const botAvatar = (id?: string) => (id && botAvatars.includes(id) ? id : botAvatars[0]);
