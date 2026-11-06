import { test, expect, type Page, type BrowserContext } from "@playwright/test";
import { randomUUID } from "node:crypto";
import pg from "pg";

const password = "correct horse battery staple";
const apiPort = process.env.E2E_API_PORT ?? "18080";
const webPort = process.env.E2E_WEB_PORT ?? "13000";
const controlPort = process.env.E2E_CONTROL_PORT ?? "18081";
const api = `http://127.0.0.1:${apiPort}`;
const origin = `http://127.0.0.1:${webPort}`;

async function connected(page: Page) { await expect(page.getByTestId("connection")).toHaveText("Connected"); }
// The server allows five sign-ups a minute per address, so wait and try again when the suite is quick.
async function signup(page: Page, username: string, avatarNumber?: number) {
  for (let attempt = 0; ; attempt++) {
    await page.goto("/signup");
    if (avatarNumber) await page.getByLabel(`Avatar ${avatarNumber}`, { exact: true }).check();
    await page.getByLabel("Username", { exact: true }).fill(username);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByLabel("Confirm password").fill(password);
    await page.getByRole("button", { name: "Create account", exact: true }).click();
    const done = page.getByTestId("connection"), limited = page.getByText("Too many attempts");
    await expect(done.or(limited)).toBeVisible();
    if (await done.isVisible()) break;
    if (attempt >= 4) throw new Error("Sign-up is still rate limited");
    await page.waitForTimeout(15_000);
  }
  await connected(page);
}
async function login(page: Page, username: string) {
  await page.goto("/signin");
  await page.getByLabel("Username", { exact: true }).fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await connected(page);
}
async function send(page: Page, text: string) {
  await page.getByRole("textbox", { name: "Message", exact: true }).fill(text);
  await page.getByRole("button", { name: "Send message" }).click();
  await expect(page.getByRole("log").locator("[data-message-id]").filter({ hasText: text })).toHaveCount(1);
}
async function direct(page: Page, username: string) {
  await page.getByRole("button", { name: `Chat with ${username}`, exact: true }).click();
  await expect(page.getByRole("heading", { name: username, exact: true })).toBeVisible();
}
async function command(page: Page, type: string, payload: object) {
  // The function body runs in the browser, so the port is passed in rather than read from Node.
  return page.evaluate(async ({ type, payload, port }) => {
    return await new Promise<{ type: string; payload: Record<string, unknown> }>((resolve, reject) => {
      const ws = new WebSocket(`ws://127.0.0.1:${port}/ws`);
      const timer = setTimeout(() => { ws.close(); reject(new Error("Command timeout")); }, 10000);
      ws.onmessage = event => {
        const frame = JSON.parse(event.data);
        if (frame.type === "session.ready") ws.send(JSON.stringify({ v: 1, type, requestId: "e2e", payload }));
        if (frame.requestId === "e2e") { clearTimeout(timer); ws.close(); resolve(frame); }
      };
    });
  }, { type, payload, port: apiPort });
}

test("real accounts, private/group routing, multiple sessions, reconnect, retries, restart and expiry", async ({ browser, request }, testInfo) => {
  const contexts: BrowserContext[] = [];
  const make = async () => { const context = await browser.newContext(); contexts.push(context); return context; };
  const aContext = await make(), bContext = await make(), cContext = await make(), a2Context = await make();
  const a = await aContext.newPage(), b = await bContext.newPage(), c = await cContext.newPage(), a2 = await a2Context.newPage();
  const errors: string[] = [];
  for (const page of [a, b, c, a2]) page.on("pageerror", e => errors.push(e.message));
  const receivedByC: string[] = [];
  c.on("websocket", ws => ws.on("framereceived", frame => receivedByC.push(String(frame.payload))));
  try {
    await signup(a, "alice"); await signup(b, "bob"); await signup(c, "carol"); await login(a2, "alice");
    // You are not in your own list: there is no one to open a private chat with.
    await expect(a.getByRole("button", { name: "Chat with alice", exact: true })).toHaveCount(0);
    await expect(b.getByRole("button", { name: "Chat with alice", exact: true })).toHaveCount(1);
    await direct(a, "bob");
    await send(a, "Private hello <script>alert('text only')</script>");
    await direct(b, "alice"); await direct(a2, "bob");
    await expect(b.getByRole("log")).toContainText("Private hello");
    await expect(a2.getByRole("log")).toContainText("Private hello");
    await expect(c.locator("[data-message-id]")).toHaveCount(0);
    const own = await command(a, "rooms.list", {});
    const rooms = own.payload.items as { id: string; kind: string }[];
    const privateID = rooms.find(room => room.kind === "direct")!.id;
    expect((await command(c, "room.history", { roomId: privateID })).payload.code).toBe("room_unavailable");
    expect((await command(c, "message.send", { roomId: privateID, clientMessageId: randomUUID(), text: "intrusion" })).payload.code).toBe("room_unavailable");

    await a.getByRole("button", { name: "New group", exact: true }).click();
    await a.getByLabel("Group name", { exact: true }).fill("Network study");
    await a.getByRole("button", { name: "Create group", exact: true }).click();
    await expect(a.getByRole("heading", { name: "Network study", exact: true })).toBeVisible();
    await expect(a.getByLabel("Group members")).toContainText("alice");
    await expect(a.getByLabel("Group members")).not.toContainText("bob");
    await send(a, "Before Bob joined");
    await b.getByRole("button", { name: "Network study Open to join", exact: true }).click();
    await expect(b.getByLabel("Group members")).toContainText("alice");
    await expect(b.getByRole("log")).toHaveCount(0);
    await b.getByRole("button", { name: "Join group", exact: true }).click();
    await expect(b.getByRole("log")).toBeVisible();
    await expect(b.getByRole("log")).not.toContainText("Before Bob joined");
    await send(b, "After joining the group");
    await expect(a.getByRole("log")).toContainText("After joining the group");
    await expect(a.getByLabel("Group members")).toContainText("bob");
    await c.getByRole("button", { name: "Network study Open to join", exact: true }).click();
    await expect(c.getByLabel("Group members")).toContainText("bob");
    await expect(c.getByRole("log")).toHaveCount(0);
    const groups = await command(a, "groups.list", {});
    const groupID = (groups.payload.items as { id: string }[])[0].id;
    expect((await command(c, "room.history", { roomId: groupID })).payload.code).toBe("room_unavailable");
    expect(receivedByC.join("\n")).not.toContain("Private hello");
    expect(receivedByC.join("\n")).not.toContain("After joining the group");

    // Room-specific drafts and message lists survive switching.
    await a.getByRole("textbox", { name: "Message", exact: true }).fill("group draft");
    await direct(a, "bob");
    await expect(a.getByRole("textbox", { name: "Message", exact: true })).toHaveValue("");
    await expect(a.getByRole("log")).not.toContainText("After joining the group");
    await a.getByRole("button", { name: /^Network study/ }).click();
    await expect(a.getByRole("textbox", { name: "Message", exact: true })).toHaveValue("group draft");
    await a.getByRole("textbox", { name: "Message", exact: true }).fill("");
    await a.screenshot({ path: testInfo.outputPath("group-desktop.png"), fullPage: true });

    // Offline device recovers history without duplicated messages.
    await direct(a2, "bob");
    await a2Context.setOffline(true);
    // Closing via a navigation also models a device that was away entirely.
    await a2.goto("about:blank");
    await direct(b, "alice");
    await send(b, "Sent while Alice device was offline");
    await a2Context.setOffline(false); await a2.goto("/"); await connected(a2); await direct(a2, "bob");
    await expect(a2.getByRole("log").locator("[data-message-id]").filter({ hasText: "Sent while Alice device was offline" })).toHaveCount(1);

    // Drop a send before it reaches Go. Explicit retry must retain its UUID.
    let dropNext = true;
    const retryIDs: string[] = [];
    await a2Context.routeWebSocket("**/ws", route => {
      const server = route.connectToServer();
      route.onMessage(data => {
        const frame = JSON.parse(String(data));
        if (frame.type === "message.send" && frame.payload.text === "Retry without duplication") {
          retryIDs.push(frame.payload.clientMessageId);
          if (dropNext) { dropNext = false; route.close({ code: 1012 }); server.close(); return; }
        }
        server.send(data);
      });
    });
    await a2.reload(); await connected(a2); await direct(a2, "bob");
    await a2.getByRole("textbox", { name: "Message", exact: true }).fill("Retry without duplication");
    await a2.getByRole("button", { name: "Send message" }).click();
    await connected(a2);
    await a2.getByRole("button", { name: "Retry message" }).click();
    await expect(b.getByRole("log").locator("[data-message-id]").filter({ hasText: "Retry without duplication" })).toHaveCount(1);
    expect(retryIDs).toHaveLength(2); expect(retryIDs[0]).toBe(retryIDs[1]);

    // Drop the storage acknowledgment and event after Go commits. History recovers it.
    await a2Context.unrouteAll();
    let lostAck = false;
    await a2Context.routeWebSocket("**/ws", route => {
      const server = route.connectToServer();
      let requestID = "";
      route.onMessage(data => {
        const frame = JSON.parse(String(data));
        if (!lostAck && frame.type === "message.send" && frame.payload.text === "Committed without acknowledgment") requestID = frame.requestId;
        server.send(data);
      });
      server.onMessage(data => {
        const frame = JSON.parse(String(data));
        if (!lostAck && requestID && frame.requestId === requestID && frame.type === "request.ok") {
          lostAck = true; route.close({ code: 1012 }); server.close(); return;
        }
        route.send(data);
      });
    });
    await a2.reload(); await connected(a2); await direct(a2, "bob");
    await a2.getByRole("textbox", { name: "Message", exact: true }).fill("Committed without acknowledgment");
    await a2.getByRole("button", { name: "Send message" }).click();
    await expect(a2.getByRole("log").locator("[data-message-id]").filter({ hasText: "Committed without acknowledgment" })).toHaveCount(1);
    await expect(a2.getByRole("button", { name: "Retry message" })).toHaveCount(0);
    expect(lostAck).toBe(true);
    await expect(b.getByRole("log").locator("[data-message-id]").filter({ hasText: "Committed without acknowledgment" })).toHaveCount(1);

    // Shared browser session logs out in all its tabs, another device survives.
    const sharedTab = await aContext.newPage(); await sharedTab.goto("/"); await connected(sharedTab);
    await a.getByRole("button", { name: "Sign out", exact: true }).click();
    await expect(a).toHaveURL(/\/signin$/);
    await expect(sharedTab.getByRole("heading", { name: "Sign in again" })).toBeVisible();
    await connected(a2);
    await expect(b.getByRole("button", { name: "Chat with alice", exact: true })).toHaveCount(1);

    // Restart actual Go (and optionally the Compose DB) with the same durable schema.
    const restarted = await request.post(`http://127.0.0.1:${controlPort}/restart`, { headers: { Authorization: `Bearer ${process.env.E2E_CONTROL_KEY}` } });
    expect(restarted.ok()).toBe(true);
    const restartResult = await restarted.json();
    await testInfo.attach("restart-result", { body: JSON.stringify(restartResult), contentType: "application/json" });
    await connected(a2); await connected(b);
    await a2.reload(); await connected(a2); await direct(a2, "bob");
    await expect(a2.getByRole("log")).toContainText("Committed without acknowledgment");
    await send(a2, "Still here after restart");
    await expect(b.getByRole("log")).toContainText("Still here after restart");
    await login(a, "alice"); // Durable account credentials still work.

    // More than one bounded history page, generated through the actual protocol.
    await b.evaluate(async ({ roomId, port }) => {
      const ws = new WebSocket(`ws://127.0.0.1:${port}/ws`);
      const waiters = new Map<string, { resolve: () => void; reject: (error: Error) => void }>();
      await new Promise<void>(resolve => {
        ws.onmessage = event => {
          const frame = JSON.parse(event.data);
          if (frame.type === "session.ready") resolve();
          const waiter = waiters.get(frame.requestId);
          if (waiter) {
            waiters.delete(frame.requestId);
            if (frame.type === "request.error") waiter.reject(new Error(frame.payload.message));
            else waiter.resolve();
          }
        };
      });
      try {
        for (let index = 0; index < 52; index++) {
          const requestId = `page_${index}`;
          const ack = new Promise<void>((resolve, reject) => waiters.set(requestId, { resolve, reject }));
          ws.send(JSON.stringify({ v: 1, type: "message.send", requestId, payload: {
            roomId, clientMessageId: crypto.randomUUID(), text: `History page message ${index}`,
          } }));
          await ack;
          await new Promise(resolve => setTimeout(resolve, 220));
        }
      } finally { ws.close(); }
    }, { roomId: privateID, port: apiPort });
    await a2.reload(); await connected(a2); await direct(a2, "bob");
    await expect(a2.getByRole("log").locator("[data-message-id]")).toHaveCount(50);
    await a2.getByRole("button", { name: "Load earlier messages" }).click();
    await expect(a2.getByRole("log")).toContainText("Private hello");
    const messageIDs = await a2.locator("[data-message-id]").evaluateAll(nodes => nodes.map(node => node.getAttribute("data-message-id")));
    expect(new Set(messageIDs).size).toBe(messageIDs.length);

    // Expire C's session in the isolated DB, then trigger command-time validation.
    const dbURL = new URL(process.env.E2E_DATABASE_URL!);
    const schema = dbURL.searchParams.get("search_path");
    if (!schema || !/^t7_[0-9a-f]{16}$/.test(schema)) throw new Error("Missing isolated test schema");
    const db = new pg.Pool({ connectionString: dbURL.href, options: `-c search_path=${schema}` });
    try {
      await db.query("UPDATE sessions SET created_at = now() - interval '8 days', expires_at = now() - interval '1 second' WHERE user_id = (SELECT id FROM users WHERE username = 'carol')");
    } finally { await db.end(); }
    await c.getByRole("button", { name: "Refresh members" }).click();
    await expect(c.getByRole("heading", { name: "Sign in again" })).toBeVisible();
    await expect(c.getByRole("log")).toHaveCount(0);

    await a2.setViewportSize({ width: 390, height: 844 });
    await a2.screenshot({ path: testInfo.outputPath("chat-mobile.png"), fullPage: true });
    expect(await a2.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    expect(errors).toEqual([]);
  } finally { await Promise.all(contexts.map(context => context.close())); }
});

// Runs with GEMINI_API_KEY empty, so no text leaves the machine. This covers the
// whole path up to the AI call; real corrections are checked by hand (see README).
test("grammar menu uses the chat socket and never sends the draft", async ({ browser }) => {
  const dianaContext = await browser.newContext(), ericContext = await browser.newContext();
  const diana = await dianaContext.newPage(), eric = await ericContext.newPage();
  const sockets: string[] = [];
  diana.on("websocket", ws => sockets.push(ws.url()));
  const sent: string[] = [];
  diana.on("websocket", ws => ws.on("framesent", frame => sent.push(String(frame.payload))));
  try {
    await signup(diana, "diana"); await signup(eric, "eric");
    await direct(diana, "eric");
    const box = diana.getByRole("textbox", { name: "Message", exact: true });
    const draft = "i has went to school";
    await box.fill(draft);

    // Without a selection the browser's own menu stays in charge.
    await box.dispatchEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 500, clientY: 500 });
    await expect(diana.getByRole("dialog", { name: "Grammar correction" })).toHaveCount(0);

    await box.evaluate((element: HTMLTextAreaElement) => element.setSelectionRange(2, 9));
    await box.dispatchEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 500, clientY: 500 });
    await diana.getByRole("button", { name: "Correct grammar" }).click();
    await expect(diana.getByRole("alert").filter({ hasText: "not configured" })).toBeVisible();

    // The composer is a form: the menu's buttons must not submit it.
    await diana.getByRole("button", { name: "Close", exact: true }).click();
    await expect(diana.getByRole("dialog", { name: "Grammar correction" })).toHaveCount(0);
    await expect(box).toHaveValue(draft);
    await expect(diana.locator("[data-message-id], .message.pending")).toHaveCount(0);
    expect(sent.filter(frame => frame.includes('"message.send"'))).toHaveLength(0);

    // One socket, the protocol envelope, and only 500 characters of context each side.
    expect(sockets).toHaveLength(1);
    const request = JSON.parse(sent.find(frame => frame.includes('"grammar.correct"'))!);
    expect(request).toMatchObject({ v: 1, type: "grammar.correct", payload: { text: "has wen", before: "i ", after: "t to school" } });
    await box.fill(`${"x".repeat(600)} teh ${"y".repeat(600)}`);
    await box.evaluate((element: HTMLTextAreaElement) => element.setSelectionRange(601, 604));
    await box.dispatchEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 500, clientY: 500 });
    await diana.getByRole("button", { name: "Correct grammar" }).click();
    await expect(diana.getByRole("alert").filter({ hasText: "not configured" })).toBeVisible();
    const trimmed = JSON.parse(sent.filter(frame => frame.includes('"grammar.correct"')).at(-1)!);
    expect(trimmed.payload).toMatchObject({ text: "teh" });
    expect(trimmed.payload.before).toHaveLength(500);
    expect(trimmed.payload.after).toHaveLength(500);
    await diana.keyboard.press("Escape");

    // A draft far above the server's frame limit must not drop the socket.
    await box.fill(`${"ก".repeat(9000)} teh`);
    await box.evaluate((element: HTMLTextAreaElement) => element.setSelectionRange(9001, 9004));
    await box.dispatchEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 500, clientY: 500 });
    await diana.getByRole("button", { name: "Correct grammar" }).click();
    await expect(diana.getByRole("alert").filter({ hasText: "not configured" })).toBeVisible();
    await diana.keyboard.press("Escape");
    await connected(diana);

    // Normal chat still works on the same connection afterwards.
    await send(diana, "Plain hello after grammar");
    await direct(eric, "diana");
    await expect(eric.getByRole("log")).toContainText("Plain hello after grammar");
    expect(sockets).toHaveLength(1);
  } finally {
    await dianaContext.close(); await ericContext.close();
  }
});

test("authentication errors and keyboard forms", async ({ page, request }) => {
  await page.goto("/signup");
  await page.getByLabel("Username", { exact: true }).fill("ชื่อไทย");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create account", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "3–24 English" })).toBeVisible();
  await page.getByLabel("Username", { exact: true }).fill("keyboard");
  await page.getByRole("button", { name: "Create account", exact: true }).click();
  await connected(page);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page.goto("/signin");
  await page.getByLabel("Username", { exact: true }).fill("keyboard");
  await page.getByLabel("Password", { exact: true }).fill("incorrect passphrase");
  await page.getByLabel("Password", { exact: true }).press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "Invalid credentials" })).toBeVisible();
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Password", { exact: true }).press("Enter");
  await connected(page);
  const rejected = await request.post(`${api}/auth/login`, { headers: { Origin: "http://untrusted.example" }, data: { username: "alice", password } });
  expect(rejected.status()).toBe(403);
  const allowed = await request.get(`${api}/auth/me`, { headers: { Origin: origin } });
  expect(allowed.status()).toBe(401); // No browser cookie in this separate request context.
});

test("shop assistant: owner configures it and a customer chats with it privately", async ({ browser }) => {
  const ownerContext = await browser.newContext(), customerContext = await browser.newContext();
  const owner = await ownerContext.newPage(), customer = await customerContext.newPage();
  try {
    await signup(owner, "shopowner"); await signup(customer, "shopper");
    await owner.getByRole("button", { name: /Manage my assistants/ }).click();
    await owner.getByRole("button", { name: "New assistant" }).click();
    await owner.getByLabel("Name", { exact: true }).fill("Corner Cafe");
    await owner.getByLabel("What is your shop about?").fill("A small coffee shop near the station.");
    await owner.getByRole("button", { name: "Add template" }).click();
    await owner.getByLabel("Question", { exact: true }).fill("What time do you open?");
    await owner.getByLabel("Answer", { exact: true }).fill("10.00");
    await owner.getByRole("button", { name: "Save assistant" }).click();
    await expect(owner.getByText("Live", { exact: true })).toBeVisible();

    // Everyone can find it, without seeing how it is configured.
    await customer.getByRole("button", { name: /Corner Cafe/ }).click();
    await expect(customer.getByRole("heading", { name: "Corner Cafe", exact: true })).toBeVisible();
    await send(customer, "What time do you open?");
    // Tests run without a Gemini key, so the assistant apologises instead of guessing.
    await expect(customer.getByRole("log")).toContainText("temporarily unavailable");

    // The owner never sees the customer's conversation.
    await owner.getByRole("button", { name: "Back to chats" }).click();
    await expect(owner.getByText("What time do you open?", { exact: true })).toHaveCount(0);
    await expect(owner.locator(".room-row.selected")).toHaveCount(0);
  } finally { await ownerContext.close(); await customerContext.close(); }
});

test("avatar choice, and the one conversation list", async ({ browser }) => {
  const contexts: BrowserContext[] = [];
  const open = async () => { const context = await browser.newContext(); contexts.push(context); return context.newPage(); };
  const suffix = Date.now().toString(36).slice(-5);
  const [dora, eli] = [`dora${suffix}`, `eli${suffix}`];
  try {
    const d = await open(), e = await open();
    // Dora picks the third picture while signing up; Eli lets the server choose.
    await signup(d, dora, 3);
    await expect(d.locator(".rail-account img")).toHaveAttribute("src", /\/avatars\/p03\.svg$/);
    await signup(e, eli);
    await expect(e.locator(".rail-account img")).toHaveAttribute("src", /\/avatars\/p(0[1-9]|1[0-2])\.svg$/);
    // People, groups and shops share one list: someone online has a picture and a green dot.
    await expect(d.getByRole("button", { name: `Chat with ${eli}`, exact: true }).locator(".presence")).toHaveCount(1);

    // A new group appears in the same list as two overlapping pictures, and anyone can join it.
    await d.getByRole("button", { name: "New group", exact: true }).click();
    await d.getByLabel("Group name", { exact: true }).fill("Reading club");
    await d.getByRole("button", { name: "Create group", exact: true }).click();
    await expect(d.getByRole("heading", { name: "Reading club", exact: true })).toBeVisible();
    await expect(d.getByRole("button", { name: "Reading club Joined", exact: true }).locator(".group-stack")).toHaveCount(1);
    await e.getByRole("button", { name: "Reading club Open to join", exact: true }).click();
    await e.getByRole("button", { name: "Join group", exact: true }).click();
    await expect(e.getByLabel("Group members")).toContainText(dora);
  } finally {
    for (const context of contexts) await context.close();
  }
});
