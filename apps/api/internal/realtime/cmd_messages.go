package realtime

import "example.com/computer-network/project/apps/api/internal/chat"

type sendRequest struct {
	RoomID          string `json:"roomId"`
	ClientMessageID string `json:"clientMessageId"`
	Text            string `json:"text"`
}

func (h *Hub) messageSend(r request, p sendRequest) (any, error) {
	result, err := h.chat.SendMessage(r.ctx, r.identity, p.RoomID, p.ClientMessageID, p.Text)
	if err != nil {
		return nil, err
	}
	m := result.Message
	h.reply(r, map[string]any{"messageId": m.ID, "roomId": m.RoomID, "seq": m.Seq, "clientMessageId": m.ClientMessageID})
	if result.Created {
		h.broadcastUsers(result.RecipientUserIDs, eventMessageCreated, m)
		if result.RoomKind == chat.RoomBot {
			h.startBot(r.identity.User.ID, m.RoomID, m.ID, m.Text)
		}
	}
	return answered{}, nil
}
