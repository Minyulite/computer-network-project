import type { User } from "../api";

export type Room = {
  id: string; kind: "direct" | "group" | "bot"; name?: string; createdBy: string;
  botId?: string; botName?: string; botAvatar?: string;
  direct?: User[]; // Both people of a direct room, so the other one is known even while offline.
  last?: { senderId: string; senderName: string; text: string; sentAt: string }; // Newest message this user may see, for the list.
  directUserLow?: string; directUserHigh?: string; latestSeq: number;
  historyStartSeq: number; createdAt: string;
};
export type Bot = { id: string; name: string; ownerId: string; ownerUsername: string; avatar?: string; enabled: boolean; createdAt: string };
export type BotTemplate = { id?: string; question: string; answer: string };
export type BotDetail = Bot & { description: string; templates: BotTemplate[] };
export type BotInput = { name: string; avatar?: string; description: string; enabled: boolean; templates: BotTemplate[] };
export type Group = { id: string; name: string; createdBy: string; createdAt: string; preview: User[] };
export type Member = { user: User; joinedAt: string };
export type Message = {
  id: string; roomId: string; sender: User; clientMessageId: string;
  seq: number; text: string; sentAt: string;
};
export type Page<T> = { items: T[]; nextCursor?: string };
export type History = {
  roomId: string; messages: Message[]; historyStartSeq: number; upToSeq: number;
  hasMore: boolean; nextBeforeSeq?: number; nextAfterSeq?: number;
};
export type PendingMessage = {
  roomId: string; clientMessageId: string; text: string; state: "sending" | "uncertain"; error?: string;
};
export type Feed = {
  messages: Message[]; through: number; before?: number; more: boolean;
  loaded: boolean; loading: boolean; error?: string;
};
export type ConnectionStatus = "connecting" | "connected" | "reconnecting" | "offline" | "expired";
export type ChatState = {
  status: ConnectionStatus; syncing: boolean; error: string; rooms: Room[]; groups: Group[]; bots: Bot[]; myBots: Bot[];
  online: User[]; known: Record<string, User>; members: Record<string, Member[]>;
  selected: string | null; feeds: Record<string, Feed>; pending: PendingMessage[];
};
export type ServerEvent =
  | { type: "session.ready"; payload: { user: User; connectionId: string; serverInstanceId: string } }
  | { type: "presence.changed"; payload: { userId: string; username: string; avatar?: string; online: boolean } }
  | { type: "group.created"; payload: { group: Group; initialMember: User } }
  | { type: "group.member_joined"; payload: { roomId: string; member: User } }
  | { type: "bot.upserted"; payload: { bot: Bot } }
  | { type: "room.upserted"; payload: { room: Room } }
  | { type: "message.created"; payload: Message };
