// @vitest-environment jsdom

import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it } from "vitest";
import { StatusBar } from "./StatusBar";
import { useStore } from "../store";
import type { NoteContent } from "@shared/ipc";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

describe("StatusBar", () => {
  it("shows the active editor line and column on the right (discussion #597)", () => {
    useStore.setState({
      notes: [],
      editorCursorPosition: { line: 3, column: 5 },
    });
    const note = {
      path: "inbox/editor-position.md",
      title: "Editor position",
      folder: "inbox",
      siblingOrder: 0,
      createdAt: 0,
      updatedAt: 0,
      size: 16,
      tags: [],
      wikilinks: [],
      assetEmbeds: [],
      hasAttachments: false,
      excerpt: "alpha",
      body: "alpha\nbeta\ngamma",
    } as NoteContent;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);

    act(() => root.render(createElement(StatusBar, { note })));

    const position = host.querySelector<HTMLElement>("[data-editor-position]");
    expect(position?.textContent).toBe("Ln 3, Col 5");
    expect(position?.getAttribute("aria-live")).toBeNull();

    act(() => root.unmount());
    host.remove();
  });
});
