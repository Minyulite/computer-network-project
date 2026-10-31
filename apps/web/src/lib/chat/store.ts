import { errorText, type User } from "../api";
import { ChatSocket, CommandError, newID } from "./socket";
import type { ChatState, Feed, Group, History, Member, Message, Page, PendingMessage, Room, ServerEvent } from "./types";

const emptyFeed = (): Feed => ({ messages: [], through: 0, more: false, loaded: false, loading: false });
const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
const upsert = <T extends { id: string }>(items: T[], value: T) => [...items.filter(item => item.id !== value.id), value];

export function mergeMessages(previous: Message[], incoming: Message[]) {
  const byID = new Map(previous.map(message => [message.id, message]));
  for (const message of incoming) byID.set(message.id, message);
  return [...byID.values()].sort((a, b) => a.seq - b.seq);
}

// One store per mounted account. Snapshots are immutable for useSyncExternalStore.
export class ChatStore {
  private state: ChatState;
  private listeners = new Set<() => void>();
  private socket: ChatSocket;
  private generation = 0;
  private buffered: ServerEvent[] | null = null;
  private refreshTask?: Promise<void>;
  private historyTasks = new Map<string, Promise<void>>();
  private memberTasks = new Map<string, Promise<void>>();
  private dirtyMembers = new Set<string>();

  constructor(readonly user: User) {
    this.state = { status: "connecting", syncing: true, error: "", rooms: [], groups: [], online: [],
      known: { [user.id]: user }, members: {}, selected: null, feeds: {}, pending: [] };
    this.socket = new ChatSocket(user.id, status => {
      if (status !== "connected") {
        this.generation++;
        this.buffered = null;
        this.refreshTask = undefined;
        this.historyTasks.clear(); this.memberTasks.clear(); this.dirtyMembers.clear();
      }
      if (status === "expired") {
        this.patch({ status, rooms: [], groups: [], online: [], feeds: {}, members: {}, pending: [], selected: null, known: {}, syncing: false });
      } else this.patch({ status, ...(status !== "connected" ? { syncing: false } : {}) });
    }, event => this.receive(event), error => this.patch({ error }));
  }

  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.state;
  start = () => this.socket.start();
  stop = () => { this.generation++; this.socket.stop(); };
  reconnect = () => this.socket.reconnect();
  clearError = () => this.patch({ error: "" });
  private patch(value: Partial<ChatState>) { this.state = { ...this.state, ...value }; this.listeners.forEach(listener => listener()); }
  private patchFeed(id: string, value: Partial<Feed>) {
    this.patch({ feeds: { ...this.state.feeds, [id]: { ...(this.state.feeds[id] ?? emptyFeed()), ...value } } });
  }
  private remember(users: User[]) {
    this.patch({ known: { ...this.state.known, ...Object.fromEntries(users.map(user => [user.id, user])) } });
  }
  private acceptMessages(roomID: string, messages: Message[]) {
    this.remember(messages.map(message => message.sender));
    const ids = new Set(messages.filter(message => message.sender.id === this.user.id).map(message => message.clientMessageId));
    this.patch({ pending: this.state.pending.filter(item => item.roomId !== roomID || !ids.has(item.clientMessageId)) });
    this.patchFeed(roomID, { messages: mergeMessages(this.state.feeds[roomID]?.messages ?? [], messages) });
  }
  private receive(event: ServerEvent) {
    if (event.type === "session.ready") { void this.refresh(); return; }
    if (this.buffered) { this.buffered.push(event); return; }
    this.applyEvent(event);
  }
  private applyEvent(event: ServerEvent) {
    switch (event.type) {
      case "presence.changed": {
        const user = { id: event.payload.userId, username: event.payload.username };
        this.remember([user]);
        this.patch({ online: event.payload.online ? upsert(this.state.online, user) : this.state.online.filter(item => item.id !== user.id) });
        break;
      }
      case "group.created":
        this.remember([event.payload.initialMember]);
        this.patch({ groups: upsert(this.state.groups, event.payload.group) });
        if (this.state.selected === event.payload.group.id) void this.loadMembers(event.payload.group.id);
        break;
      case "group.member_joined": {
        const { roomId, member } = event.payload;
        this.remember([member]);
        // Refetch with timestamps; never fabricate membership metadata.
        if (this.state.selected === roomId) {
          if (this.memberTasks.has(roomId)) this.dirtyMembers.add(roomId);
          else void this.loadMembers(roomId);
        }
        else {
          const members = { ...this.state.members };
          delete members[roomId]; this.patch({ members });
        }
        break;
      }
      case "room.upserted":
        this.patch({ rooms: upsert(this.state.rooms, event.payload.room) });
        break;
      case "message.created": {
        const message = event.payload;
        this.acceptMessages(message.roomId, [message]);
        const room = this.state.rooms.find(room => room.id === message.roomId);
        if (room) this.patch({ rooms: upsert(this.state.rooms, { ...room, latestSeq: Math.max(room.latestSeq, message.seq) }) });
        // Advance only across contiguous sequences. A later event never hides a gap.
        const feed = this.state.feeds[message.roomId];
        if (feed.loaded) {
          const sequences = new Set(feed.messages.map(item => item.seq));
          let through = feed.through;
          while (sequences.has(through + 1)) through++;
          this.patchFeed(message.roomId, { through });
        }
        break;
      }
    }
  }

  private async read<T>(type: string, payload: object = {}): Promise<T> {
    for (let attempt = 0; ; attempt++) {
      try { return await this.socket.request<T>(type, payload); }
      catch (e) {
        if (!(e instanceof CommandError) || e.code !== "rate_limited" || attempt >= 3) throw e;
        await delay(250 * (attempt + 1));
      }
    }
  }
  private async pages<T>(type: string, generation: number, payload: object = {}) {
    const result: T[] = [];
    let cursor: string | undefined;
    do {
      if (generation !== this.generation) throw new Error("Connection changed.");
      const page = await this.read<Page<T>>(type, { ...payload, limit: 50, ...(cursor ? { cursor } : {}) });
      result.push(...page.items);
      cursor = page.nextCursor;
      if (cursor) await delay(75);
    } while (cursor);
    return result;
  }

  refresh = (): Promise<void> => {
    if (this.refreshTask) return this.refreshTask;
    const generation = this.generation;
    this.buffered = [];
    this.patch({ syncing: true, error: "" });
    const task = (async () => {
      try {
        const online = await this.pages<User>("users.list", generation);
        const groups = await this.pages<Group>("groups.list", generation);
        const rooms = await this.pages<Room>("rooms.list", generation);
        if (generation !== this.generation) return;
        this.remember(online);
        this.patch({ online, groups, rooms, members: {} });
      } catch (e) {
        if (generation === this.generation) this.patch({ error: `Could not refresh conversations. ${errorText(e)}` });
      } finally {
        if (generation === this.generation) {
          const events = this.buffered ?? [];
          this.buffered = null;
          for (const event of events) this.applyEvent(event);
          this.patch({ syncing: false }); this.refreshTask = undefined;
          if (this.state.selected) this.select(this.state.selected);
        }
      }
    })();
    this.refreshTask = task;
    return task;
  };

  select = (id: string) => {
    this.patch({ selected: id, error: "" });
    if (this.state.status !== "connected" || this.state.syncing) return;
    if (this.state.rooms.some(room => room.id === id)) void this.loadHistory(id);
    if (this.state.groups.some(group => group.id === id)) void this.loadMembers(id);
  };
  private async roomCommand(type: string, payload: object) {
    const generation = this.generation;
    const { room } = await this.socket.request<{ room: Room }>(type, payload);
    if (generation !== this.generation) return;
    this.patch({ rooms: upsert(this.state.rooms, room) });
    this.select(room.id);
  }
  openDirect = (user: User) => { this.remember([user]); return this.roomCommand("direct.open", { receiverId: user.id }); };
  createGroup = (name: string) => this.roomCommand("group.create", { name });
  joinGroup = (roomId: string) => this.roomCommand("group.join", { roomId });

  loadMembers = (roomId: string): Promise<void> => {
    const existing = this.memberTasks.get(roomId);
    if (existing) return existing;
    const generation = this.generation;
    const task = (async () => {
      try {
        const members = await this.pages<Member>("groups.members", generation, { roomId });
        if (generation !== this.generation) return;
        this.remember(members.map(member => member.user));
        this.patch({ members: { ...this.state.members, [roomId]: members } });
      } catch (e) {
        if (generation === this.generation) this.patch({ error: `Could not load members. ${errorText(e)}` });
      } finally {
        if (generation === this.generation) {
          this.memberTasks.delete(roomId);
          if (this.dirtyMembers.delete(roomId)) void this.loadMembers(roomId);
        }
      }
    })();
    this.memberTasks.set(roomId, task); return task;
  };

  loadHistory = (roomId: string, older = false): Promise<void> => {
    const existing = this.historyTasks.get(roomId);
    if (existing) return existing;
    const generation = this.generation;
    const original = this.state.feeds[roomId] ?? emptyFeed();
    this.patchFeed(roomId, { loading: true, error: undefined });
    const task = (async () => {
      try {
        if (!original.loaded || older) {
          const page = await this.read<History>("room.history", { roomId, limit: 50,
            ...(older && original.before ? { beforeSeq: original.before } : {}) });
          if (generation !== this.generation) return;
          this.acceptMessages(roomId, page.messages);
          this.patchFeed(roomId, { loaded: true, more: page.hasMore, before: page.nextBeforeSeq,
            through: original.loaded ? this.state.feeds[roomId].through : page.upToSeq });
        } else {
          let afterSeq = original.through;
          let upToSeq: number | undefined;
          for (;;) {
            const page: History = await this.read<History>("room.history", { roomId, limit: 50, afterSeq,
              ...(upToSeq === undefined ? {} : { upToSeq }) });
            if (generation !== this.generation) return;
            upToSeq = page.upToSeq;
            this.acceptMessages(roomId, page.messages);
            if (!page.hasMore) {
              this.patchFeed(roomId, { through: Math.max(this.state.feeds[roomId].through, page.upToSeq) });
              break;
            }
            if (page.nextAfterSeq === undefined || page.nextAfterSeq <= afterSeq) throw new Error("History cursor did not advance.");
            afterSeq = page.nextAfterSeq;
            await delay(75);
          }
        }
      } catch (e) {
        if (generation === this.generation) this.patchFeed(roomId, { error: errorText(e) });
      } finally {
        if (generation === this.generation) { this.patchFeed(roomId, { loading: false }); this.historyTasks.delete(roomId); }
      }
    })();
    this.historyTasks.set(roomId, task); return task;
  };

  send = async (roomId: string, text: string, retryID?: string) => {
    const item: PendingMessage = { roomId, text, clientMessageId: retryID ?? newID(), state: "sending" };
    const generation = this.generation;
    this.patch({ pending: [...this.state.pending.filter(p => p.clientMessageId !== item.clientMessageId), item] });
    try {
      const ack = await this.socket.request<{ messageId: string; seq: number }>("message.send", {
        roomId, text, clientMessageId: item.clientMessageId,
      });
      if (generation !== this.generation) return;
      // Retried messages receive an acknowledgment without another broadcast.
      // Read that exact persisted message, including its authoritative timestamp.
      if (!this.state.feeds[roomId]?.messages.some(message => message.id === ack.messageId)) {
        const page = await this.read<History>("room.history", { roomId, afterSeq: ack.seq - 1, upToSeq: ack.seq, limit: 1 });
        if (generation === this.generation) this.acceptMessages(roomId, page.messages);
      }
    } catch (e) {
      if (this.state.status === "expired") return;
      this.patch({ pending: this.state.pending.map(p => p.clientMessageId === item.clientMessageId ? { ...p, state: "uncertain", error: errorText(e) } : p) });
    }
  };
}
