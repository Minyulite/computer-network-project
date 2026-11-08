"use client";

import { useEffect, useState, type FormEvent } from "react";
import { Modal } from "@/components/modal";
import { AvatarPicker } from "@/components/avatar";
import { PlusIcon, Spinner } from "@/components/icons";
import { errorText } from "@/lib/api";
import { botAvatar, botAvatars, randomOf } from "@/lib/avatars";
import type { ChatStore } from "@/lib/chat/store";
import type { BotInput } from "@/lib/chat/types";

export const maxBots = 5;
const limits = { bots: maxBots, templates: 20, name: 64, description: 2000, question: 300, answer: 1000 };
const emptyForm = (avatar = botAvatars[0]): BotInput => ({ name: "", avatar, description: "", enabled: true, templates: [] });
const count = (value: string) => Array.from(value.trim()).length;

type Props = { store: ChatStore; botId: string | "new"; connected: boolean; onClose: () => void };

// Owner's editor for one shop assistant: description and answer templates. Opens as a modal.
export function BotEditor({ store, botId, connected, onClose }: Props) {
  const [form, setForm] = useState<BotInput>(() => emptyForm(randomOf(botAvatars)));
  const [loading, setLoading] = useState(botId !== "new");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (botId === "new") return;
    let live = true;
    store.getBot(botId).then(detail => {
      if (live) setForm({ name: detail.name, avatar: botAvatar(detail.avatar), description: detail.description, enabled: detail.enabled, templates: detail.templates });
    }).catch(e => { if (live) setError(errorText(e)); }).finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [store, botId]);

  function setTemplate(index: number, patch: Partial<BotInput["templates"][number]>) {
    setForm(value => ({ ...value, templates: value.templates.map((t, i) => i === index ? { ...t, ...patch } : t) }));
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const templates = form.templates.map(t => ({ question: t.question.trim(), answer: t.answer.trim() }))
      .filter(t => t.question || t.answer);
    if (!count(form.name) || count(form.name) > limits.name) { setError("Give the assistant a name of 1–64 characters."); return; }
    if (count(form.description) > limits.description) { setError("The shop description is limited to 2,000 characters."); return; }
    if (templates.some(t => !t.question || !t.answer || Array.from(t.question).length > limits.question || Array.from(t.answer).length > limits.answer)) {
      setError("Every template needs a question (up to 300 characters) and an answer (up to 1,000)."); return;
    }
    setBusy(true); setError("");
    try {
      await store.saveBot(botId === "new" ? undefined : botId,
        { name: form.name.trim(), avatar: form.avatar, description: form.description.trim(), enabled: form.enabled, templates });
      onClose();
    } catch (e) { setError(errorText(e)); setBusy(false); }
  }

  return <Modal wide title={botId === "new" ? "New shop bot" : "Edit shop bot"} onClose={() => { if (!busy) onClose(); }}>
    <p className="bot-note">Customers chat with your assistant privately; you do not see their conversations. Their messages and your description are sent to Google Gemini to write answers. Replies to template questions are your own words, unchanged.</p>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="loading-row"><Spinner label="Loading" /></p> :
    <form className="bot-form" onSubmit={save}>
      <AvatarPicker ids={botAvatars} name="bot-avatar" label="Avatar" value={form.avatar ?? botAvatars[0]} onChange={avatar => setForm({ ...form, avatar })} />
      <label>Name<input value={form.name} maxLength={limits.name} required onChange={e => setForm({ ...form, name: e.target.value })} placeholder="Corner Café" /></label>
      <label>What is your shop about?
        <textarea rows={6} value={form.description} maxLength={limits.description} onChange={e => setForm({ ...form, description: e.target.value })}
          placeholder="What you sell, where you are, opening hours, prices, policies. The assistant answers only from this text and your templates." /></label>
      <small className="muted figure field-count">{count(form.description).toLocaleString()}/{limits.description.toLocaleString()}</small>

      <fieldset className="bot-templates"><legend>Answer templates <small className="count">{form.templates.length}/{limits.templates}</small></legend>
        <p className="small muted">When a customer asks something similar to a question, the assistant sends your answer exactly as written.</p>
        {form.templates.map((t, i) => <div className="bot-template" key={i}>
          <label>Question<input value={t.question} maxLength={limits.question} onChange={e => setTemplate(i, { question: e.target.value })} placeholder="What time do you open?" /></label>
          <label>Answer<textarea rows={2} value={t.answer} maxLength={limits.answer} onChange={e => setTemplate(i, { answer: e.target.value })} placeholder="We open at 10:00, Monday to Saturday" /></label>
          <button type="button" className="text-button" onClick={() => setForm(value => ({ ...value, templates: value.templates.filter((_, j) => j !== i) }))}>Remove</button>
        </div>)}
        <button type="button" className="with-icon add-template" disabled={form.templates.length >= limits.templates}
          onClick={() => setForm(value => ({ ...value, templates: [...value.templates, { question: "", answer: "" }] }))}><PlusIcon />Add template</button>
      </fieldset>

      {botId !== "new" && <label className="bot-toggle"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })} /> Live, so customers can find and chat with it</label>}
      <div className="bot-actions">
        <button className="primary" disabled={!connected || busy}>{busy ? <Spinner label="Saving" /> : "Save assistant"}</button>
        <button type="button" disabled={busy} onClick={onClose}>Cancel</button>
      </div>
    </form>}
  </Modal>;
}
