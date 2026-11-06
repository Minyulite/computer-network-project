"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useSyncExternalStore, type FormEvent, type ReactNode } from "react";
import { Avatar, GroupAvatar, PersonAvatar } from "@/components/avatar";
import { BotManager } from "@/components/bot-manager";
import { GrammarTextarea } from "@/components/grammar-textarea";
import { Brand, BrandMark, ChatIcon, LanguagesIcon, LogOutIcon, SearchIcon, SendIcon, SlidersIcon, StoreIcon, UserIcon, UserPlusIcon, UsersIcon } from "@/components/icons";
import { ApiError, authRequest, errorText, type Session, type User } from "@/lib/api";
import { botAvatar, personAvatar } from "@/lib/avatars";
import { ChatStore } from "@/lib/chat/store";
import type { Bot, ChatState, Group, Room } from "@/lib/chat/types";
import { convertKeyboardLanguage } from "@/lib/chat/keyboard-language";

export function ChatApp() {
  const router = useRouter();
  const [session, setSession] = useState<Session | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    void authRequest<Session>("/auth/me").then(value => { if (active) setSession(value); }).catch(e => {
      if (!active) return;
      if (e instanceof ApiError && e.status === 401) router.replace("/signin");
      else setError(errorText(e));
    });
    return () => { active = false; };
  }, [router, attempt]);
  if (session) return <ChatWorkspace key={session.user.id} user={session.user} />;
  return <main className="loading-shell"><Brand />
    {error ? <><p role="alert">{error}</p><button onClick={() => { setError(""); setAttempt(attempt + 1); }}>Try again</button></> : <p className="muted" role="status">Opening your conversations…</p>}</main>;
}

function otherUserID(room: Room, user: User) {
  return room.directUserLow === user.id ? room.directUserHigh : room.directUserLow;
}

// The other person of a direct room. The server names both, so this works while they are offline.
function peerOf(room: Room, state: ChatState, user: User): User {
  const id = otherUserID(room, user) ?? "";
  return room.direct?.find(person => person.id === id) ?? state.known[id] ?? { id, username: `Private chat · ${id.slice(0, 8)}` };
}

function roomName(room: Room, state: ChatState, user: User) {
  if (room.kind === "group") return room.name ?? "Group";
  if (room.kind === "bot") return room.botName ?? "Shop assistant";
  return peerOf(room, state, user).username;
}

const formatTime = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });

type View = "all" | "people" | "groups" | "shops";
const views: { id: View; label: string; icon: ReactNode }[] = [
  { id: "all", label: "All chats", icon: <ChatIcon size={20} /> },
  { id: "people", label: "People", icon: <UserIcon size={20} /> },
  { id: "groups", label: "Groups", icon: <UsersIcon size={20} /> },
  { id: "shops", label: "Shops", icon: <StoreIcon size={20} /> },
];

// The newest message of a conversation, shown under its name in the list.
type Last = { own: boolean; who?: string; text: string; at: string };

// Everything you can open in one list: people, shop assistants and groups.
type Row =
  | { kind: "person"; key: string; name: string; user: User; roomId?: string; online: boolean; last: Last | null }
  | { kind: "shop"; key: string; name: string; bot: Bot; roomId?: string; last: Last | null }
  | { kind: "group"; key: string; name: string; group: Group; joined: boolean; last: Last | null };

// "now", "20m", "3h", "2d", then a date: short enough to sit beside a name.
function relativeTime(iso: string, now: number) {
  const seconds = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return days < 7 ? `${days}d` : new Date(iso).toLocaleDateString([], { month: "short", day: "numeric" });
}

// Re-renders now and then so the times in the list stay current.
function useNow(intervalMs = 30_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), intervalMs); return () => clearInterval(timer); }, [intervalMs]);
  return now;
}

const isActive = (row: Row) => row.kind === "group" ? row.joined : !!row.roomId;
const lastTime = (row: Row) => row.last ? Date.parse(row.last.at) : 0;

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null);
  // A native modal dialog traps focus, closes on Escape and blocks the page behind it.
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="dialog" aria-labelledby="dialog-title" onClose={onClose}
    onClick={event => { if (event.target === dialog.current) onClose(); }}>
    <h2 id="dialog-title">{title}</h2>{children}
  </dialog>;
}

type DialogProps = { uncertain?: boolean; onSubmit: (name: string) => Promise<void>; onRefresh: () => Promise<void>; onClose: () => void };

function GroupDialog({ uncertain, onSubmit, onRefresh, onClose }: DialogProps) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function run(task: () => Promise<void>) {
    setBusy(true); setError("");
    try { await task(); } catch (e) { setError(errorText(e)); } finally { setBusy(false); }
  }
  function submitForm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || Array.from(trimmed).length > 64) { setError("Group names need 1–64 characters."); return; }
    void run(() => onSubmit(trimmed));
  }
  return <Modal title="Create a group" onClose={onClose}>
    <form onSubmit={submitForm}>
      <label>Group name<input value={name} onChange={e => setName(e.target.value)} required autoFocus /></label>
      {error && <p className="error" role="alert">{error}</p>}
      {uncertain && <div className="small"><p>Creation was not confirmed. Refresh and check the group list before trying again.</p>
        <button type="button" disabled={busy} onClick={() => void run(onRefresh)}>Refresh group list</button></div>}
      <div className="dialog-actions">
        <button type="button" onClick={onClose}>Cancel</button>
        <button className="primary" disabled={busy || !!uncertain}>{busy ? "Please wait…" : "Create group"}</button>
      </div>
    </form>
  </Modal>;
}

function ChatWorkspace({ user }: { user: User }) {
  const [store] = useState(() => new ChatStore(user));
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  const router = useRouter();
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createUncertain, setCreateUncertain] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState("");
  const [managing, setManaging] = useState(false);
  const [view, setView] = useState<View>("all");
  const now = useNow();
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => { store.start(); return () => store.stop(); }, [store]);
  const room = state.rooms.find(room => room.id === state.selected);
  const group = state.groups.find(group => group.id === state.selected);
  const feed = state.selected ? state.feeds[state.selected] : undefined;
  const lastMessageID = feed?.messages.at(-1)?.id;
  useEffect(() => { end.current?.scrollIntoView({ block: "nearest" }); }, [lastMessageID, state.selected, state.pending.length]);
  const connected = state.status === "connected" && !state.syncing;
  const selectedTitle = room ? roomName(room, state, user) : group?.name;
  const draft = state.selected ? drafts[state.selected] ?? "" : "";
  const draftLength = Array.from(draft.trim()).length;
  const overLimit = draftLength > 2000;
  const pending = state.pending.filter(item => item.roomId === state.selected);
  const online = new Set(state.online.map(person => person.id));
  const othersOnline = state.online.filter(person => person.id !== user.id).length;

  // The newest message: from the loaded history if there is any, else what the server sent with the room.
  function lastOf(item: Room | undefined): Last | null {
    if (!item) return null;
    const message = state.feeds[item.id]?.messages.at(-1);
    if (message) return { own: message.sender.id === user.id, who: message.sender.username, text: message.text, at: message.sentAt };
    const last = item.last;
    return last ? { own: last.senderId === user.id, who: last.senderName, text: last.text, at: last.sentAt } : null;
  }

  // One list for everything, so nothing is hidden behind a section heading.
  const rows: Row[] = [];
  const seen = new Set<string>();
  for (const item of state.rooms) {
    if (item.kind !== "direct") continue;
    const peer = peerOf(item, state, user);
    seen.add(peer.id);
    rows.push({ kind: "person", key: item.id, name: peer.username, user: peer, roomId: item.id, online: online.has(peer.id), last: lastOf(item) });
  }
  for (const person of state.online) {
    if (person.id !== user.id && !seen.has(person.id)) rows.push({ kind: "person", key: `user-${person.id}`, name: person.username, user: person, online: true, last: null });
  }
  for (const bot of state.bots) {
    const botRoom = state.rooms.find(item => item.kind === "bot" && item.botId === bot.id);
    rows.push({ kind: "shop", key: `bot-${bot.id}`, name: bot.name, bot, roomId: botRoom?.id, last: lastOf(botRoom) });
  }
  for (const item of state.groups) {
    const groupRoom = state.rooms.find(r => r.id === item.id);
    rows.push({ kind: "group", key: item.id, name: item.name, group: item, joined: !!groupRoom, last: lastOf(groupRoom) });
  }
  const shownKinds = view === "all" ? ["person", "shop", "group"] : view === "people" ? ["person"] : view === "groups" ? ["group"] : ["shop"];
  const shown = rows.filter(row => shownKinds.includes(row.kind) && row.name.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => Number(isActive(b)) - Number(isActive(a)) || lastTime(b) - lastTime(a) ||
      Number(b.kind === "person" && b.online) - Number(a.kind === "person" && a.online) || a.name.localeCompare(b.name));

  async function action(fn: () => Promise<unknown>) {
    setBusy(true); setActionError("");
    try { await fn(); } catch (e) { setActionError(errorText(e)); } finally { setBusy(false); }
  }
  function choose(id: string) { setManaging(false); store.select(id); }
  async function chatWithBot(botId: string) { setManaging(false); await action(() => store.openBot(botId)); }
  async function logout() {
    await authRequest("/auth/logout", {});
    store.stop(); router.replace("/signin");
  }
  async function createGroup(name: string) {
    try { await store.createGroup(name); } catch (e) { setCreateUncertain(true); throw e; }
    setCreating(false); setManaging(false);
  }
  function openRow(row: Row) {
    if (row.kind === "person") { if (row.roomId) choose(row.roomId); else void action(() => store.openDirect(row.user)); }
    else if (row.kind === "shop") void chatWithBot(row.bot.id);
    else choose(row.group.id);
  }
  function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!room || !connected) return;
    const text = draft.trim();
    if (!text || Array.from(text).length > 2000) { setActionError("Messages need 1–2,000 characters."); return; }
    setActionError(""); setDrafts(value => ({ ...value, [room.id]: "" }));
    void store.send(room.id, text);
  }

  function convertDraft(textarea: HTMLTextAreaElement | null) {
    if (!textarea || !room) return;
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    const from = start === end ? 0 : start;
    const to = start === end ? textarea.value.length : end;
    if (from === to) return;
    const converted = convertKeyboardLanguage(textarea.value.slice(from, to));
    const next = textarea.value.slice(0, from) + converted + textarea.value.slice(to);
    setDrafts(value => ({ ...value, [room.id]: next }));
    requestAnimationFrame(() => { textarea.focus(); textarea.setSelectionRange(from, from + converted.length); });
  }
  if (state.status === "expired") return <main className="loading-shell"><Brand /><h1>Sign in again</h1>
    <p className="muted">Your session ended, or another account signed in on this browser.</p><Link className="button primary" href="/signin">Sign in</Link></main>;

  const botOwner = room?.kind === "bot" ? state.bots.find(bot => bot.id === room.botId)?.ownerUsername : undefined;
  const members = group ? state.members[group.id] : undefined;
  const subtitle = managing ? "Your shop"
    : group ? (members ? `${members.length} ${members.length === 1 ? "member" : "members"}, ${members.filter(member => online.has(member.user.id)).length} online` : "Group")
    : room?.kind === "bot" ? (botOwner ? `Shop assistant by ${botOwner}` : "Shop assistant")
    : room ? (online.has(otherUserID(room, user) ?? "") ? "Online" : "Offline")
    : undefined;
  const connection = state.syncing ? "Syncing…" : state.status === "connected" ? "Connected" : state.status === "connecting" ? "Connecting…" : state.status === "reconnecting" ? "Reconnecting…" : "Offline";

  return <main className="workspace">
    <nav className="rail" aria-label="Sections">
      <span className="rail-logo" role="img" aria-label="Commonroom"><BrandMark size={24} /></span>
      {views.map(item => <button key={item.id} className="rail-item" aria-pressed={view === item.id && !managing}
        onClick={() => { setManaging(false); setView(item.id); }}>{item.icon}{item.label}</button>)}
      <span className="rail-spacer" />
      <span className="rail-divider" />
      <div className="rail-account" title={user.username}><PersonAvatar user={user} size="sm" /><span>{user.username}</span></div>
      <button className="rail-item" aria-label="Manage my assistants" aria-pressed={managing} onClick={() => setManaging(true)}><SlidersIcon size={20} />My shops</button>
      <button className="rail-item" onClick={() => void action(logout)} disabled={busy}><LogOutIcon size={20} />Sign out</button>
    </nav>

    <div className="panel">
      <aside className="sidebar" aria-label="Conversations">
        <div className="list-tools">
          <label className="search-label"><span className="sr-only">Find people or groups</span><SearchIcon size={18} />
            <input type="search" placeholder="Search" aria-label="Find people or groups" value={filter} onChange={e => setFilter(e.target.value)} /></label>
          <button className="icon-button" aria-label="New group" title="New group" disabled={!connected} onClick={() => setCreating(true)}><UserPlusIcon size={20} /></button>
        </div>
        <div className="sidebar-scroll">
          {shown.map(row => {
            const selected = row.kind === "group" ? state.selected === row.group.id : !!row.roomId && state.selected === row.roomId;
            const disabled = row.kind !== "group" && (!connected || busy) && !(row.kind === "person" && row.roomId);
            return <button className={`room-row ${selected ? "selected" : ""}`} key={row.key} aria-pressed={selected} disabled={disabled}
              aria-label={row.kind === "person" ? `Chat with ${row.name}` : undefined} onClick={() => openRow(row)}>
              {row.kind === "person" ? <PersonAvatar user={row.user} online={row.online} />
                : row.kind === "shop" ? <Avatar id={botAvatar(row.bot.avatar)} bot />
                : <GroupAvatar members={row.group.preview ?? []} />}
              <span className="row-body">
                <span className="row-top"><span className="row-title">{row.name}</span>
                  {row.last && <time className="row-time" dateTime={row.last.at}>{relativeTime(row.last.at, now)}</time>}</span>
                <span className="row-sub">{row.last ? <>{row.last.own ? <span className="you">You: </span> : row.kind === "group" && row.last.who ? `${row.last.who}: ` : null}{row.last.text}</>
                  : row.kind === "person" ? (row.online ? "Online" : "Offline")
                  : row.kind === "shop" ? `Shop assistant by ${row.bot.ownerUsername}` : row.joined ? "Joined" : "Open to join"}</span>
              </span>
            </button>;
          })}
          {shown.length === 0 && <p className="empty-note">{filter ? "Nothing matches your search" : connected ? "No one is here yet. Create a group, or wait for people to come online" : "Waiting for connection…"}</p>}
        </div>
      </aside>

      <section className="chat-column" aria-label="Chat">
        <header className="chat-header"><div className="chat-title"><h1>{managing ? "Shop assistants" : selectedTitle ?? "Conversations"}</h1>
          {subtitle && <p className="subtitle">{subtitle}</p>}</div>
          <div className="header-actions">
            <div className="connection" role="status" data-testid="connection"><i className={connected ? "presence" : "presence offline"} />{connection}</div>
          </div>
        </header>
        {state.status !== "connected" && <div className="notice">Your drafts are kept while the connection returns <button onClick={store.reconnect}>Reconnect now</button></div>}
        {(actionError || state.error) && <div className="notice error" role="alert">{actionError || state.error} <button onClick={() => { setActionError(""); store.clearError(); }}>Dismiss</button>
          {state.error && <button disabled={!connected} onClick={() => void store.refresh()}>Refresh conversations</button>}</div>}
        {managing ? <BotManager store={store} bots={state.myBots} connected={connected} onClose={() => setManaging(false)} onOpenBot={id => void chatWithBot(id)} /> :
        !state.selected ? <div className="welcome"><h2>Pick someone to talk to</h2>
          <p>Choose a person for a private chat, open a group to read along and join, or ask a shop&apos;s assistant a question.</p>
          <p className="welcome-online"><i className={othersOnline ? "presence" : "presence offline"} />{othersOnline ? `${othersOnline} ${othersOnline === 1 ? "person" : "people"} online now` : "No one else is online yet"}</p></div> :
          <>
            {group && <div className="members" aria-label="Group members"><strong>Members</strong>
              {state.members[group.id] ? state.members[group.id].map(member => <span key={member.user.id}>{online.has(member.user.id) && <i className="presence" />}{member.user.username}</span>) : <span>Loading members…</span>}
              <button className="text-button" disabled={!connected} onClick={() => void store.loadMembers(group.id)}>Refresh members</button></div>}
            {!room ? <div className="welcome"><h2>Join {group?.name ?? "this group"}</h2><p>Members can send messages and read everything posted after they join.</p>
              <button className="primary large" disabled={!connected || busy} onClick={() => void action(() => store.joinGroup(state.selected!))}>Join group</button></div> : <>
              <div className="message-list" role="log" aria-label={`Messages in ${selectedTitle}`} aria-live="polite">
                {feed?.more && <button className="history-button" disabled={!connected || feed.loading} onClick={() => void store.loadHistory(room.id, true)}>Load earlier messages</button>}
                {feed?.loading && <p className="center muted" role="status">Loading messages…</p>}
                {feed?.error && <p className="error" role="alert">{feed.error} <button disabled={!connected} onClick={() => void store.loadHistory(room.id)}>Retry history</button></p>}
                {feed?.loaded && !feed.messages.length && <div className="empty-conversation"><p>No messages yet</p><span>{group ? "You will see messages posted after you joined" : "Send the first message"}</span></div>}
                {feed?.messages.map((message, index) => {
                  const own = message.sender.id === user.id;
                  const name = own ? "You" : room.kind === "bot" ? room.botName ?? "Assistant" : message.sender.username;
                  const next = feed.messages[index + 1];
                  const tail = !next || next.sender.id !== message.sender.id; // The picture sits beside the last bubble of a run.
                  const picture = room.kind === "bot" && !own ? botAvatar(room.botAvatar) : personAvatar(own ? user : message.sender);
                  const avatar = <Avatar id={picture} size="sm" ghost={!tail} />;
                  return <article className={`message ${own ? "own" : ""}`} key={message.id} data-message-id={message.id}>
                    {!own && avatar}
                    <div className="bubble">
                      {room.kind === "group" && !own ? <strong className="sender">{name}</strong> : <span className="sr-only">{name}: </span>}
                      <p>{message.text}</p>
                      {message.sentAt && <time dateTime={message.sentAt} title={new Date(message.sentAt).toLocaleString()}>{formatTime(message.sentAt)}</time>}
                    </div>
                    {own && avatar}
                  </article>;
                })}
                {pending.map(item => <article className="message own pending" key={item.clientMessageId}><div className="bubble"><p>{item.text}</p>
                  <small>{item.state === "sending" ? "Saving…" : "Not confirmed"}</small>
                  {item.state === "uncertain" && <><p className="small">{item.error}</p><button disabled={!connected} onClick={() => void store.send(item.roomId, item.text, item.clientMessageId)}>Retry message</button></>}</div></article>)}
                {room.kind === "bot" && (feed?.messages.at(-1)?.sender.id === user.id || pending.some(item => item.state === "sending")) &&
                  <p className="typing" role="status">{room.botName ?? "The assistant"} is typing…</p>}
                <div ref={end} />
              </div>
              <form className="composer" onSubmit={send}>
                <label className="sr-only" htmlFor="message">Message</label>
                <div className={`composer-box ${overLimit ? "over" : ""}`} title={overLimit ? "Messages are limited to 2,000 characters" : undefined}>
                  {/* Keyed so a correction started in one room can never land in another room's draft. */}
                  <GrammarTextarea key={room.id} id="message" placeholder="Your message" value={draft} rows={1}
                    onChange={text => setDrafts(value => ({ ...value, [room.id]: text }))}
                    onConvert={convertDraft}
                    correct={store.correctGrammar} canCorrect={connected} />
                  <button type="button" className="icon-button quiet" aria-label="Convert language" title="Convert selected text or the whole draft (Ctrl+Shift)" disabled={!draft}
                    onClick={e => convertDraft(e.currentTarget.form?.querySelector("textarea") ?? null)}><LanguagesIcon size={20} /></button>
                  <button className="primary send" aria-label="Send message" disabled={!connected || !draft.trim() || overLimit}><SendIcon size={20} /></button>
                </div>
              </form>
            </>}
          </>}
      </section>
    </div>
    {creating && <GroupDialog uncertain={createUncertain} onSubmit={createGroup}
      onRefresh={async () => { await store.refresh(); if (!store.getSnapshot().error) setCreateUncertain(false); }} onClose={() => setCreating(false)} />}
  </main>;
}
