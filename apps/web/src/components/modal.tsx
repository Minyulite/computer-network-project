"use client";

import { useEffect, useRef, type ReactNode } from "react";

export function Modal({ title, onClose, wide, children }: { title: string; onClose: () => void; wide?: boolean; children: ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null);
  // A native modal dialog traps focus, closes on Escape and blocks the page behind it.
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className={`dialog${wide ? " wide" : ""}`} aria-labelledby="dialog-title" onClose={onClose}
    onClick={event => { if (event.target === dialog.current) onClose(); }}>
    <h2 id="dialog-title">{title}</h2>{children}
  </dialog>;
}
