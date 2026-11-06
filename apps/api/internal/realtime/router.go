package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
)

const commandTimeout = 3 * time.Second

// request is one command from a client whose session was just revalidated.
type request struct {
	ctx      context.Context
	client   *client
	id       string // The envelope's requestId, echoed in the reply.
	identity auth.Identity
}

// answered is returned by a handler that already queued its reply itself,
// either to broadcast events after it or because the reply comes later.
type answered struct{}

// handlerFunc runs one command. Its result is sent as request.ok unless it is
// answered{}; an error is sent as request.error.
type handlerFunc func(h *Hub, r request, payload json.RawMessage) (any, error)

type route struct {
	handle handlerFunc
	limit  *rateLimit // Extra per-command limit on top of commandLimit.
}

var (
	sendLimit    = rateLimit{rate: 5, burst: 10}
	grammarLimit = rateLimit{rate: 0.5, burst: 5}
)

var routes = map[string]route{
	"users.list":      {handle: handle((*Hub).usersList)},
	"rooms.list":      {handle: handle((*Hub).roomsList)},
	"groups.list":     {handle: handle((*Hub).groupsList)},
	"groups.members":  {handle: handle((*Hub).groupMembers)},
	"direct.open":     {handle: handle((*Hub).directOpen)},
	"group.create":    {handle: handle((*Hub).groupCreate)},
	"group.join":      {handle: handle((*Hub).groupJoin)},
	"room.history":    {handle: handle((*Hub).roomHistory)},
	"message.send":    {handle: handle((*Hub).messageSend), limit: &sendLimit},
	"bots.list":       {handle: handle((*Hub).botsList)},
	"bots.mine":       {handle: handle((*Hub).botsMine)},
	"bot.get":         {handle: handle((*Hub).botGet)},
	"bot.create":      {handle: handle((*Hub).botCreate)},
	"bot.update":      {handle: handle((*Hub).botUpdate)},
	"bot.open":        {handle: handle((*Hub).botOpen)},
	"grammar.correct": {handle: handle((*Hub).grammarCorrect), limit: &grammarLimit},
}

// handle adapts a handler taking a typed payload. Unknown fields and trailing
// data are rejected as invalid_payload before the handler runs.
func handle[P any](fn func(h *Hub, r request, p P) (any, error)) handlerFunc {
	return func(h *Hub, r request, raw json.RawMessage) (any, error) {
		var p P
		if err := payloadInto(raw, &p); err != nil {
			return nil, err
		}
		return fn(h, r, p)
	}
}

func (h *Hub) handleCommand(cmd command) {
	c := cmd.client
	if h.connections[c.id] == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
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
	rt, known := routes[cmd.envelope.Type]
	if !h.ratesFor(identity.User.ID).allow(time.Now(), cmd.envelope.Type, rt.limit) {
		h.fail(c, cmd.envelope.RequestID, auth.ErrRateLimited)
		return
	}
	if !known {
		h.fail(c, cmd.envelope.RequestID, errUnknownCommand)
		return
	}
	response, err := rt.handle(h, request{ctx: ctx, client: c, id: cmd.envelope.RequestID, identity: identity}, cmd.envelope.Payload)
	switch {
	case err != nil:
		h.fail(c, cmd.envelope.RequestID, err)
	case response != (answered{}):
		h.ok(c, cmd.envelope.RequestID, response)
	}
}

func (h *Hub) ratesFor(userID string) *userRates {
	rates := h.rates[userID]
	if rates == nil {
		rates = &userRates{}
		h.rates[userID] = rates
	}
	return rates
}

func (h *Hub) ok(c *client, id string, payload any) { h.enqueue(c, wire(frameOK, id, payload)) }

func (h *Hub) fail(c *client, id string, err error) {
	h.enqueue(c, wire(frameError, id, protocolErrorFor(err)))
}

// reply queues the answer for r now, so events broadcast afterwards arrive
// after it. The handler then returns answered{}.
func (h *Hub) reply(r request, payload any) { h.ok(r.client, r.id, payload) }
