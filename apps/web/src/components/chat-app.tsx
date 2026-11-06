"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useSyncExternalStore, type FormEvent } from "react";
import { BotManager } from "@/components/bot-manager";
import { GrammarTextarea } from "@/components/grammar-textarea";
import { Brand, PlusIcon, SearchIcon, StoreIcon } from "@/components/icons";
import { ApiError, authRequest, errorText, type Session, type User } from "@/lib/api";
import { ChatStore } from "@/lib/chat/store";
import type { ChatState, Room } from "@/lib/chat/types";
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

function roomName(room: Room, state: ChatState, user: User) {
  if (room.kind === "group") return room.name ?? "Group";
  if (room.kind === "bot") return room.botName ?? "Shop assistant";
  const other = otherUserID(room, user);
  return state.known[other ?? ""]?.username ?? `Private chat · ${other?.slice(0, 8)}`;
}

// By code point, so an emoji or a Thai letter with its vowel mark is never cut in half.
const initials = (name: string) => Array.from(name).slice(0, 2).join("").toUpperCase();
const formatTime = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });

function ChatWorkspace({ user }: { user: User }) {
  const [store] = useState(() => new ChatStore(user));
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  const router = useRouter();
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState(false);
  const [groupName, setGroupName] = useState("");
  const [createUncertain, setCreateUncertain] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState("");
  const [managing, setManaging] = useState(false);
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
  const pending = state.pending.filter(item => item.roomId === state.selected);
  const visible = (name: string) => name.toLowerCase().includes(filter.toLowerCase());
  const othersOnline = state.online.filter(person => person.id !== user.id).length;
  const online = new Set(state.online.map(person => person.id));

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
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = groupName.trim();
    if (!name || Array.from(name).length > 64) { setActionError("Group names need 1–64 characters."); return; }
    await action(async () => {
      try { await store.createGroup(name); setGroupName(""); }
      catch (e) { setCreateUncertain(true); throw e; }
    });
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

  const kicker = managing ? "Your shop" : group ? "Group" : room?.kind === "bot" ? "Shop assistant" : room ? "Private conversation" : undefined;
  const connection = state.syncing ? "Syncing…" : state.status === "connected" ? "Connected" : state.status === "connecting" ? "Connecting…" : state.status === "reconnecting" ? "Reconnecting…" : "Offline";

  return <main className="workspace">
    <aside className="sidebar" aria-label="Conversations">
      <div className="sidebar-top"><Brand /></div>
      <label className="search-label"><span className="sr-only">Find people or groups</span><SearchIcon />
        <input type="search" placeholder="Find people or groups" value={filter} onChange={e => setFilter(e.target.value)} /></label>
      <div className="sidebar-scroll">
        <section aria-labelledby="online-heading"><h2 id="online-heading">Online <span className="count">{state.online.length}</span></h2>
          {!state.online.length && <p className="empty-note">{connected ? "No one online yet" : "Waiting for connection…"}</p>}
          {state.online.filter(person => visible(person.username)).sort((a, b) => a.username.localeCompare(b.username)).map(person =>
            <button className="person-row" key={person.id} disabled={!connected || busy || person.id === user.id}
              onClick={() => void action(() => store.openDirect(person))} aria-label={person.id === user.id ? `${person.username} (you)` : `Chat with ${person.username}`}>
              <span className="avatar">{initials(person.username)}<i className="presence" /></span><span className="row-label">{person.username}</span>
              {person.id === user.id && <small className="row-meta">you</small>}
            </button>)}
        </section>
        <section aria-labelledby="private-heading"><h2 id="private-heading">Private conversations</h2>
          {!state.rooms.some(room => room.kind === "direct") && <p className="empty-note">Choose someone online to start one</p>}
          {state.rooms.filter(room => room.kind === "direct" && visible(roomName(room, state, user))).map(room => {
            const name = roomName(room, state, user);
            return <button className={`room-row ${state.selected === room.id ? "selected" : ""}`} key={room.id} aria-pressed={state.selected === room.id} onClick={() => choose(room.id)}>
              <span className="avatar" aria-hidden>{initials(name)}{online.has(otherUserID(room, user) ?? "") && <i className="presence" />}</span><span className="row-label">{name}</span>
            </button>;
          })}
        </section>
        <section aria-labelledby="bots-heading"><h2 id="bots-heading">Shop assistants <span className="count">{state.bots.length}</span></h2>
          {!state.bots.length && <p className="empty-note">No shops have an assistant yet</p>}
          {state.bots.filter(bot => visible(bot.name)).map(bot => {
            const botRoom = state.rooms.find(item => item.kind === "bot" && item.botId === bot.id);
            return <button className={`room-row ${botRoom && state.selected === botRoom.id ? "selected" : ""}`} key={bot.id}
              aria-pressed={!!botRoom && state.selected === botRoom.id} disabled={!connected || busy} onClick={() => void chatWithBot(bot.id)}>
              <span className="room-symbol"><StoreIcon />{bot.enabled && <i className="presence" />}</span><span className="row-label">{bot.name}<small>by {bot.ownerUsername}</small></span>
            </button>;
          })}
          <button className="text-button" onClick={() => setManaging(true)}>Manage my assistants <span className="count">{state.myBots.length}</span></button>
        </section>
        <section aria-labelledby="groups-heading"><h2 id="groups-heading">Discover groups <span className="count">{state.groups.length}</span></h2>
          {!state.groups.length && <p className="empty-note">No groups yet. Create the first one below</p>}
          {state.groups.filter(group => visible(group.name)).map(group =>
            <button className={`room-row ${state.selected === group.id ? "selected" : ""}`} key={group.id} aria-pressed={state.selected === group.id} onClick={() => choose(group.id)}>
              <span className="room-symbol">#</span><span className="row-label">{group.name}<small>{state.rooms.some(room => room.id === group.id) ? "Joined" : "Open to join"}</small></span>
            </button>)}
          <form className="group-form" onSubmit={create}><label htmlFor="group-name">Create a group</label>
            <div className="inline"><input id="group-name" placeholder="Group name" value={groupName} onChange={e => setGroupName(e.target.value)} required />
              <button className="icon-button" aria-label="Create group" disabled={!connected || busy || createUncertain}><PlusIcon /></button></div>
            {createUncertain && <div className="small"><p>Creation was not confirmed. Refresh and check the group list before trying again.</p>
              <button type="button" disabled={!connected} onClick={() => void action(async () => { await store.refresh(); if (!store.getSnapshot().error) setCreateUncertain(false); })}>Refresh group list</button></div>}
          </form>
        </section>
      </div>
      <div className="account"><span className="avatar">{initials(user.username)}</span><span className="row-label"><strong>{user.username}</strong></span>
        <button className="text-button" onClick={() => void action(logout)} disabled={busy}>Sign out</button></div>
    </aside>

    <section className="chat-column" aria-label="Chat">
      <header className="chat-header"><div className="chat-title">{kicker && <p className="kicker">{kicker}</p>}
        <h1>{managing ? "Shop assistants" : selectedTitle ?? "Conversations"}</h1></div>
        <div className="connection" role="status" data-testid="connection"><i className={connected ? "presence" : "presence offline"} />{connection}</div>
      </header>
      {state.status !== "connected" && <div className="notice">Your drafts are kept while the connection returns <button onClick={store.reconnect}>Reconnect now</button></div>}
      {(actionError || state.error) && <div className="notice error" role="alert">{actionError || state.error} <button onClick={() => { setActionError(""); store.clearError(); }}>Dismiss</button>
        {state.error && <button disabled={!connected} onClick={() => void store.refresh()}>Refresh conversations</button>}</div>}
      {managing ? <BotManager store={store} bots={state.myBots} connected={connected} onClose={() => setManaging(false)} onOpenBot={id => void chatWithBot(id)} /> :
      !state.selected ? <div className="welcome"><h2>Pick someone to talk to</h2>
        <p>Choose a person who is online for a private chat, open a group to read along and join, or ask a shop&apos;s assistant a question.</p>
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
              {feed?.messages.map(message => <article className={`message ${message.sender.id === user.id ? "own" : ""}`} key={message.id} data-message-id={message.id}>
                <div className="message-meta"><strong>{message.sender.id === user.id ? "You" : room.kind === "bot" ? room.botName ?? "Assistant" : message.sender.username}</strong>
                  {message.sentAt && <time dateTime={message.sentAt} title={new Date(message.sentAt).toLocaleString()}>{formatTime(message.sentAt)}</time>}</div>
                <p>{message.text}</p></article>)}
              {pending.map(item => <article className="message own pending" key={item.clientMessageId}><p>{item.text}</p>
                <small>{item.state === "sending" ? "Saving…" : "Not confirmed"}</small>
                {item.state === "uncertain" && <><p className="small">{item.error}</p><button disabled={!connected} onClick={() => void store.send(item.roomId, item.text, item.clientMessageId)}>Retry message</button></>}</article>)}
              {room.kind === "bot" && (feed?.messages.at(-1)?.sender.id === user.id || pending.some(item => item.state === "sending")) &&
                <p className="typing" role="status">{room.botName ?? "The assistant"} is typing…</p>}
              <div ref={end} />
            </div>
            <form className="composer" onSubmit={send}>
              <label className="sr-only" htmlFor="message">Message</label>
              {/* Keyed so a correction started in one room can never land in another room's draft. */}
              <GrammarTextarea key={room.id} id="message" placeholder={`Message ${selectedTitle}`} value={draft} rows={2}
                onChange={text => setDrafts(value => ({ ...value, [room.id]: text }))}
                onConvert={convertDraft}
                correct={store.correctGrammar} canCorrect={connected} />
              <div className="composer-footer"><span className={draftLength > 2000 ? "error" : "muted"}><span className="figure">{draftLength.toLocaleString()}/2,000</span> <span className="keyboard-hint">· <kbd>Enter</kbd> to send, <kbd>Shift</kbd> <kbd>Enter</kbd> for a new line</span></span>
                <div className="composer-actions"><button type="button" title="Convert selected text or the whole draft (Ctrl+Shift)" disabled={!draft} onClick={e => convertDraft(e.currentTarget.form?.querySelector("textarea") ?? null)}>Convert language <kbd>Ctrl</kbd><kbd>Shift</kbd></button>
                  <button className="primary" aria-label="Send message" disabled={!connected || !draft.trim() || draftLength > 2000}>Send</button></div></div>
            </form>
          </>}
        </>}
    </section>
  </main>;
}
