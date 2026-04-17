// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { flushSync, mount, unmount, tick } from "svelte";
// @ts-ignore
import SessionBreadcrumb from "./SessionBreadcrumb.svelte";
// @ts-ignore
import SessionBreadcrumbTestHost from "./SessionBreadcrumbTestHost.svelte";
import type { Session } from "../../api/types.js";
import { getSessionDirectory, listOpeners, resumeSession } from "../../api/client.js";

vi.mock("../../api/client.js", () => ({
  listOpeners: vi.fn().mockResolvedValue({ openers: [] }),
  getSessionDirectory: vi.fn().mockResolvedValue({ path: "" }),
  resumeSession: vi.fn(),
  openSession: vi.fn(),
}));

vi.mock("../../utils/clipboard.js", () => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
}));

type SessionWithTokenFlags = Session & {
  has_peak_context_tokens?: boolean;
  has_total_output_tokens?: boolean;
};

function makeSession(
  agent: string,
  overrides: Partial<SessionWithTokenFlags> = {},
): SessionWithTokenFlags {
  return {
    id: "run:123456789abcdef",
    project: "proj-a",
    machine: "mac",
    agent,
    first_message: "hello",
    started_at: "2026-02-20T12:30:00Z",
    ended_at: "2026-02-20T12:31:00Z",
    message_count: 2,
    user_message_count: 1,
    total_output_tokens: 0,
    peak_context_tokens: 0,
    created_at: "2026-02-20T12:30:00Z",
    ...overrides,
  };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("SessionBreadcrumb", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(listOpeners).mockResolvedValue({ openers: [] });
    vi.mocked(getSessionDirectory).mockResolvedValue({ path: "" });
  });

  it("renders gemini with rose badge color", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("gemini"),
        onBack: () => {},
      },
    });

    await tick();
    const badge = document.querySelector(".agent-badge");
    expect(badge).toBeTruthy();
    expect(badge?.getAttribute("style")).toContain("var(--accent-rose)");

    unmount(component);
  });

  it("falls back to blue for unknown agents", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("unknown"),
        onBack: () => {},
      },
    });

    await tick();
    const badge = document.querySelector(".agent-badge");
    expect(badge?.getAttribute("style")).toContain("var(--accent-blue)");

    unmount(component);
  });

  describe("copy-link timer", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("restarts timer on rapid re-copy", async () => {
      const component = mount(SessionBreadcrumb, {
        target: document.body,
        props: {
          session: makeSession("claude"),
          onBack: () => {},
        },
      });
      await tick();

      const linkBtn = document.querySelector(".link-btn");
      expect(linkBtn).toBeTruthy();

      // First copy
      linkBtn!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await tick();
      await vi.advanceTimersByTimeAsync(0);
      await tick();
      expect(linkBtn!.classList.contains("link-btn--copied")).toBe(true);

      // Advance 1s, then copy again
      await vi.advanceTimersByTimeAsync(1000);
      linkBtn!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await tick();
      await vi.advanceTimersByTimeAsync(0);
      await tick();

      // 600ms after second click — first timer's 1.5s
      // would have expired, but it was cleared
      await vi.advanceTimersByTimeAsync(600);
      await tick();
      expect(linkBtn!.classList.contains("link-btn--copied")).toBe(true);

      // After full 1.5s from second click, state clears
      await vi.advanceTimersByTimeAsync(900);
      await tick();
      expect(linkBtn!.classList.contains("link-btn--copied")).toBe(false);

      unmount(component);
    });
  });

  it("renders compact token totals when both token metrics are reported", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude", {
          peak_context_tokens: 2400,
          total_output_tokens: 180,
          has_peak_context_tokens: true,
          has_total_output_tokens: true,
        }),
        onBack: () => {},
      },
    });

    await tick();
    const tokenBadge = document.querySelector(".token-badge");
    expect(tokenBadge?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      "2.4k ctx / 180 out",
    );

    unmount(component);
  });

  it("renders an explicit missing token placeholder when context tokens are absent", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude", {
          peak_context_tokens: 0,
          total_output_tokens: 180,
          has_peak_context_tokens: false,
          has_total_output_tokens: true,
        }),
        onBack: () => {},
      },
    });

    await tick();
    const tokenBadge = document.querySelector(".token-badge");
    expect(tokenBadge?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      "— ctx / 180 out",
    );

    unmount(component);
  });

  it("renders a dedicated mobile token badge", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude", {
          peak_context_tokens: 2400,
          total_output_tokens: 180,
          has_peak_context_tokens: true,
          has_total_output_tokens: true,
        }),
        onBack: () => {},
      },
    });

    await tick();

    const mobileTokenBadge = document.querySelector(".token-badge--mobile");
    expect(mobileTokenBadge?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      "2.4k ctx / 180 out",
    );

    unmount(component);
  });

  it("hides local-only actions for remote sessions", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude", {
          id: "devbox1~abc-123",
          machine: "devbox1",
        }),
        onBack: () => {},
      },
    });

    await tick();

    // The dropdown trigger (.resume-btn) should not appear
    // for remote sessions (no resume, no copy-dir, no open-in).
    const resumeBtn = document.querySelector(".resume-btn");
    expect(resumeBtn).toBeNull();

    unmount(component);
  });

  it("shows Claude Desktop resume for cowork without copy command", async () => {
    vi.mocked(listOpeners).mockResolvedValueOnce({
      openers: [
        {
          id: "claude-desktop",
          name: "Claude Desktop",
          kind: "action",
          bin: "/Applications/Claude.app",
        },
      ],
    });

    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude-cowork", {
          id: "claude-cowork:local_123",
        }),
        onBack: () => {},
      },
    });

    await tick();
    await Promise.resolve();
    await tick();

    const trigger = document.querySelector(
      ".resume-btn",
    ) as HTMLButtonElement | null;
    expect(trigger?.textContent).toContain("Resume");
    trigger?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();

    const items = Array.from(document.querySelectorAll(".open-menu-name")).map(
      (el) => el.textContent?.trim(),
    );
    expect(items).toContain("Claude Desktop (macOS only)");
    expect(items).not.toContain("Copy command");
    expect(items).not.toContain("Default terminal");

    const desktopItem = Array.from(
      document.querySelectorAll(".open-menu-item"),
    ).find((el) => el.textContent?.includes("Claude Desktop")) as
      | HTMLButtonElement
      | undefined;
    expect(desktopItem).toBeTruthy();

    vi.mocked(resumeSession).mockResolvedValueOnce({
      launched: true,
      terminal: "Claude Desktop",
      command: "",
      cwd: "",
    });
    desktopItem?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await tick();

    expect(resumeSession).toHaveBeenCalledWith("claude-cowork:local_123", {
      opener_id: "claude-desktop",
    });

    unmount(component);
  });

  it("hides cowork resume when Claude Desktop is unavailable", async () => {
    const component = mount(SessionBreadcrumb, {
      target: document.body,
      props: {
        session: makeSession("claude-cowork", {
          id: "claude-cowork:local_123",
        }),
        onBack: () => {},
      },
    });

    await tick();

    const trigger = document.querySelector(".resume-btn");
    expect(trigger).toBeNull();

    unmount(component);
  });

  it("refetches cowork directory when the same session updates", async () => {
    vi.mocked(getSessionDirectory)
      .mockResolvedValueOnce({ path: "" })
      .mockResolvedValueOnce({ path: "/tmp/project" });

    const component = mount(SessionBreadcrumbTestHost, {
      target: document.body,
      props: {
        initialSession: makeSession("claude-cowork", {
          id: "claude-cowork:local_123",
          file_mtime: 1,
        }),
      },
    }) as { setSession: (session: SessionWithTokenFlags) => void };

    await tick();
    await Promise.resolve();
    await tick();

    const firstCallCount = vi.mocked(getSessionDirectory).mock.calls.length;
    expect(firstCallCount).toBeGreaterThan(0);

    flushSync(() => {
      component.setSession(makeSession("claude-cowork", {
        id: "claude-cowork:local_123",
        file_mtime: 2,
      }));
    });
    await tick();
    await Promise.resolve();
    await tick();

    expect(getSessionDirectory).toHaveBeenCalledTimes(firstCallCount + 1);

    unmount(component);
  });
});
