import type { User } from "../api";

export type Room = {
  id: string; kind: "direct" | "group"; name?: string; createdBy: string;
  directUserLow?: string; directUserHigh?: string; latestSeq: number;
  historyStartSeq: number; createdAt: string;
};
export type Group = { id: string; name: string; createdBy: string; createdAt: string };
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
  status: ConnectionStatus; syncing: boolean; error: string; rooms: Room[]; groups: Group[];
  online: User[]; known: Record<string, User>; members: Record<string, Member[]>;
  selected: string | null; feeds: Record<string, Feed>; pending: PendingMessage[];
};
export type ServerEvent =
  | { type: "session.ready"; payload: { user: User; connectionId: string; serverInstanceId: string } }
  | { type: "presence.changed"; payload: { userId: string; username: string; online: boolean } }
  | { type: "group.created"; payload: { group: Group; initialMember: User } }
  | { type: "group.member_joined"; payload: { roomId: string; member: User } }
  | { type: "room.upserted"; payload: { room: Room } }
  | { type: "message.created"; payload: Message };
