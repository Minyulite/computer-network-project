package realtime

import "example.com/computer-network/project/apps/api/internal/chat"

type pageRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

func (p pageRequest) options() chat.PageOptions {
	return chat.PageOptions{Limit: p.Limit, Cursor: p.Cursor}
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

func (h *Hub) usersList(_ request, p pageRequest) (any, error) {
	return h.onlinePage(p.Limit, p.Cursor)
}

func (h *Hub) usersAll(r request, p pageRequest) (any, error) {
	return h.chat.People(r.ctx, r.identity, p.options())
}

func (h *Hub) roomsList(r request, p pageRequest) (any, error) {
	return h.chat.OwnRooms(r.ctx, r.identity, p.options())
}

func (h *Hub) groupsList(r request, p pageRequest) (any, error) {
	return h.chat.Groups(r.ctx, r.identity, p.options())
}

func (h *Hub) groupMembers(r request, p membersRequest) (any, error) {
	return h.chat.GroupMembers(r.ctx, r.identity, p.RoomID, chat.PageOptions{Limit: p.Limit, Cursor: p.Cursor})
}

func (h *Hub) directOpen(r request, p directRequest) (any, error) {
	room, created, err := h.chat.OpenDirect(r.ctx, r.identity, p.ReceiverID)
	if err != nil {
		return nil, err
	}
	h.reply(r, map[string]any{"room": room, "created": created})
	if created {
		h.broadcastUsers([]string{room.DirectUserLow, room.DirectUserHigh}, eventRoomUpserted, map[string]any{"room": room})
	}
	return answered{}, nil
}

func (h *Hub) groupCreate(r request, p createRequest) (any, error) {
	room, err := h.chat.CreateGroup(r.ctx, r.identity, p.Name)
	if err != nil {
		return nil, err
	}
	h.reply(r, map[string]any{"room": room})
	h.broadcastAll(eventGroupCreated, map[string]any{
		"group":         chat.Group{ID: room.ID, Name: room.Name, CreatedBy: room.CreatedBy, CreatedAt: room.CreatedAt, Preview: []chat.UserSummary{userSummary(r)}},
		"initialMember": userSummary(r),
	})
	h.broadcastUsers([]string{r.identity.User.ID}, eventRoomUpserted, map[string]any{"room": room})
	return answered{}, nil
}

func (h *Hub) groupJoin(r request, p roomRequest) (any, error) {
	result, err := h.chat.JoinGroup(r.ctx, r.identity, p.RoomID)
	if err != nil {
		return nil, err
	}
	h.reply(r, map[string]any{"room": result.Room, "joined": result.Created})
	if result.Created {
		h.broadcastAll(eventGroupMemberJoined, map[string]any{"roomId": result.Room.ID, "member": userSummary(r)})
		h.broadcastUsers([]string{r.identity.User.ID}, eventRoomUpserted, map[string]any{"room": result.Room})
	}
	return answered{}, nil
}

func (h *Hub) roomHistory(r request, p historyRequest) (any, error) {
	page, err := h.chat.History(r.ctx, r.identity, p.RoomID,
		chat.HistoryOptions{Limit: p.Limit, BeforeSeq: p.BeforeSeq, AfterSeq: p.AfterSeq, UpToSeq: p.UpToSeq})
	if err != nil {
		return nil, err
	}
	return fitHistory(page, p.AfterSeq != nil, r.id), nil
}

// fitHistory drops messages from the far end of the page until its frame fits
// responseLimit, then points the continuation cursor at what was dropped.
func fitHistory(page chat.HistoryPage, forward bool, requestID string) chat.HistoryPage {
	for len(page.Messages) > 1 && len(wire(frameOK, requestID, page)) > responseLimit {
		page.HasMore = true
		if forward {
			page.Messages = page.Messages[:len(page.Messages)-1]
		} else {
			page.Messages = page.Messages[1:]
		}
	}
	page.SetContinuation(forward)
	return page
}

func userSummary(r request) chat.UserSummary {
	return chat.UserSummary{ID: r.identity.User.ID, Username: r.identity.User.Username, Avatar: r.identity.User.Avatar}
}
