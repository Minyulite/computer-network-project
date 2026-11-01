package realtime

import (
	"context"
	"errors"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
)

type pageRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}
type roomRequest struct {
	RoomID string `json:"roomId"`
}
type membersRequest struct {
	RoomID string `json:"roomId"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}
type historyRequest struct {
	RoomID    string `json:"roomId"`
	Limit     int    `json:"limit"`
	BeforeSeq *int64 `json:"beforeSeq"`
	AfterSeq  *int64 `json:"afterSeq"`
	UpToSeq   *int64 `json:"upToSeq"`
}
type directRequest struct {
	ReceiverID string `json:"receiverId"`
}
type createRequest struct {
	Name string `json:"name"`
}
type sendRequest struct {
	RoomID          string `json:"roomId"`
	ClientMessageID string `json:"clientMessageId"`
	Text            string `json:"text"`
}

func (h *Hub) handleCommand(cmd command) {
	c := cmd.client
	if h.connections[c.id] == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	identity, err := h.auth.ValidateSessionID(ctx, c.identity.SessionID)
	if errors.Is(err, auth.ErrUnauthenticated) || (err == nil && identity.User.ID != c.identity.User.ID) {
		h.removeClient(c, 1008, "Session unavailable")
		return
	}
	if err != nil {
		h.fail(c, cmd.envelope.RequestID, err)
		return
	}
	rates := h.rates[identity.User.ID]
	if rates == nil {
		rates = &userRates{}
		h.rates[identity.User.ID] = rates
	}
	now := time.Now()
	if !rates.all.allow(now, 20, 40) || (cmd.envelope.Type == "message.send" && !rates.sends.allow(now, 5, 10)) {
		h.fail(c, cmd.envelope.RequestID, auth.ErrRateLimited)
		return
	}
	var response any
	switch cmd.envelope.Type {
	case "users.list":
		var p pageRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			response, err = h.onlinePage(p.Limit, p.Cursor)
		}
	case "rooms.list":
		var p pageRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			response, err = h.chat.OwnRooms(ctx, identity, chat.PageOptions{Limit: p.Limit, Cursor: p.Cursor})
		}
	case "groups.list":
		var p pageRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			response, err = h.chat.Groups(ctx, identity, chat.PageOptions{Limit: p.Limit, Cursor: p.Cursor})
		}
	case "groups.members":
		var p membersRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			response, err = h.chat.GroupMembers(ctx, identity, p.RoomID, chat.PageOptions{Limit: p.Limit, Cursor: p.Cursor})
		}
	case "direct.open":
		var p directRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			var room chat.Room
			var created bool
			room, created, err = h.chat.OpenDirect(ctx, identity, p.ReceiverID)
			if err == nil {
				response = map[string]any{"room": room, "created": created}
				h.ok(c, cmd.envelope.RequestID, response)
				if created {
					h.broadcastUsers([]string{room.DirectUserLow, room.DirectUserHigh}, "room.upserted", map[string]any{"room": room})
				}
				return
			}
		}
	case "group.create":
		var p createRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			var room chat.Room
			room, err = h.chat.CreateGroup(ctx, identity, p.Name)
			if err == nil {
				response = map[string]any{"room": room}
				h.ok(c, cmd.envelope.RequestID, response)
				h.broadcastAll("group.created", map[string]any{"group": chat.Group{ID: room.ID, Name: room.Name, CreatedBy: room.CreatedBy, CreatedAt: room.CreatedAt}, "initialMember": chat.UserSummary{ID: identity.User.ID, Username: identity.User.Username}})
				h.broadcastUsers([]string{identity.User.ID}, "room.upserted", map[string]any{"room": room})
				return
			}
		}
	case "group.join":
		var p roomRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			var result chat.JoinResult
			result, err = h.chat.JoinGroup(ctx, identity, p.RoomID)
			if err == nil {
				response = map[string]any{"room": result.Room, "joined": result.Created}
				h.ok(c, cmd.envelope.RequestID, response)
				if result.Created {
					h.broadcastAll("group.member_joined", map[string]any{"roomId": result.Room.ID, "member": chat.UserSummary{ID: identity.User.ID, Username: identity.User.Username}})
					h.broadcastUsers([]string{identity.User.ID}, "room.upserted", map[string]any{"room": result.Room})
				}
				return
			}
		}
	case "message.send":
		var p sendRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			var result chat.SendResult
			result, err = h.chat.SendMessage(ctx, identity, p.RoomID, p.ClientMessageID, p.Text)
			if err == nil {
				h.ok(c, cmd.envelope.RequestID, map[string]any{"messageId": result.Message.ID, "roomId": result.Message.RoomID, "seq": result.Message.Seq, "clientMessageId": result.Message.ClientMessageID})
				if result.Created {
					h.broadcastUsers(result.RecipientUserIDs, "message.created", result.Message)
				}
				return
			}
		}
	case "room.history":
		var p historyRequest
		if err = payloadInto(cmd.envelope.Payload, &p); err == nil {
			var page chat.HistoryPage
			page, err = h.chat.History(ctx, identity, p.RoomID, chat.HistoryOptions{Limit: p.Limit, BeforeSeq: p.BeforeSeq, AfterSeq: p.AfterSeq, UpToSeq: p.UpToSeq})
			if err == nil {
				response = fitHistory(page, p.AfterSeq != nil, cmd.envelope.RequestID)
			}
		}
	default:
		h.enqueue(c, wire("request.error", cmd.envelope.RequestID, protocolError{"unknown_type", "Unknown command"}))
		return
	}
	if err != nil {
		h.fail(c, cmd.envelope.RequestID, err)
		return
	}
	h.ok(c, cmd.envelope.RequestID, response)
}

func (h *Hub) ok(c *client, id string, payload any) { h.enqueue(c, wire("request.ok", id, payload)) }

func (h *Hub) fail(c *client, id string, err error) {
	code, message := "internal_error", "Request failed"
	switch {
	case errors.Is(err, errEnvelope), errors.Is(err, chat.ErrInvalidInput):
		code, message = "invalid_payload", "Invalid payload"
	case errors.Is(err, auth.ErrUnauthenticated), errors.Is(err, chat.ErrUnauthenticated):
		code, message = "unauthenticated", "Authentication required"
	case errors.Is(err, chat.ErrRoomUnavailable):
		code, message = "room_unavailable", "Room unavailable"
	case errors.Is(err, chat.ErrNotGroup):
		code, message = "not_a_group", "Not a group"
	case errors.Is(err, chat.ErrIdempotencyConflict):
		code, message = "idempotency_conflict", "Message ID conflicts with earlier send"
	case errors.Is(err, chat.ErrUserUnavailable):
		code, message = "user_unavailable", "User unavailable"
	case errors.Is(err, chat.ErrSelfChat):
		code, message = "self_chat", "Cannot open a direct room with yourself"
	case errors.Is(err, chat.ErrSequenceExhausted):
		code, message = "sequence_exhausted", "Room sequence exhausted"
	case errors.Is(err, auth.ErrRateLimited):
		code, message = "rate_limited", "Too many requests"
	}
	h.enqueue(c, wire("request.error", id, protocolError{code, message}))
}

func fitHistory(page chat.HistoryPage, forward bool, id string) chat.HistoryPage {
	for len(page.Messages) > 1 && len(wire("request.ok", id, page)) > responseLimit {
		page.HasMore = true
		if forward {
			page.Messages = page.Messages[:len(page.Messages)-1]
		} else {
			page.Messages = page.Messages[1:]
		}
	}
	if page.HasMore && len(page.Messages) > 0 {
		if forward {
			value := page.Messages[len(page.Messages)-1].Seq
			page.NextAfterSeq = &value
			page.NextBeforeSeq = nil
		} else {
			value := page.Messages[0].Seq
			page.NextBeforeSeq = &value
			page.NextAfterSeq = nil
		}
	}
	return page
}
