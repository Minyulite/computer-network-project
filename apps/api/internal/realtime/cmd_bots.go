package realtime

import "example.com/computer-network/project/apps/api/internal/chat"

type botIDRequest struct {
	BotID string `json:"botId"`
}
type botCreateRequest struct {
	Name        string                  `json:"name"`
	Avatar      string                  `json:"avatar"`
	Description string                  `json:"description"`
	Templates   []chat.BotTemplateInput `json:"templates"`
}
type botUpdateRequest struct {
	BotID       string                  `json:"botId"`
	Name        string                  `json:"name"`
	Avatar      string                  `json:"avatar"`
	Description string                  `json:"description"`
	Enabled     bool                    `json:"enabled"`
	Templates   []chat.BotTemplateInput `json:"templates"`
}

func (h *Hub) botsList(r request, p pageRequest) (any, error) {
	return h.chat.BotCatalog(r.ctx, r.identity, p.options())
}

func (h *Hub) botsMine(r request, _ struct{}) (any, error) {
	bots, err := h.chat.MyBots(r.ctx, r.identity)
	return map[string]any{"bots": bots}, err
}

func (h *Hub) botGet(r request, p botIDRequest) (any, error) {
	return h.chat.GetBot(r.ctx, r.identity, p.BotID)
}

func (h *Hub) botCreate(r request, p botCreateRequest) (any, error) {
	detail, err := h.chat.CreateBot(r.ctx, r.identity, chat.BotInput{Name: p.Name, Avatar: p.Avatar, Description: p.Description, Enabled: true, Templates: p.Templates})
	return h.botSaved(r, detail, err)
}

func (h *Hub) botUpdate(r request, p botUpdateRequest) (any, error) {
	detail, err := h.chat.UpdateBot(r.ctx, r.identity, p.BotID, chat.BotInput{Name: p.Name, Avatar: p.Avatar, Description: p.Description, Enabled: p.Enabled, Templates: p.Templates})
	return h.botSaved(r, detail, err)
}

// botSaved answers the owner with the full bot and tells everyone about its public part.
func (h *Hub) botSaved(r request, detail chat.BotDetail, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	h.reply(r, detail)
	h.broadcastAll(eventBotUpserted, map[string]any{"bot": detail.Bot})
	return answered{}, nil
}

func (h *Hub) botOpen(r request, p botIDRequest) (any, error) {
	room, created, err := h.chat.OpenBot(r.ctx, r.identity, p.BotID)
	if err != nil {
		return nil, err
	}
	h.reply(r, map[string]any{"room": room, "created": created})
	if created {
		h.broadcastUsers([]string{r.identity.User.ID}, eventRoomUpserted, map[string]any{"room": room})
	}
	return answered{}, nil
}
