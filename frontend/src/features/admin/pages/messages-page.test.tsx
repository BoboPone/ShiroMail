import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAdminMessages } from "../api";
import { AdminMessagesPage } from "./messages-page";

vi.mock("../api", () => ({
  fetchAdminMessages: vi.fn(),
}));

describe("AdminMessagesPage", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();

    vi.mocked(fetchAdminMessages).mockResolvedValue([
      {
        id: 1,
        subject: "=?UTF-8?B?5rWL6K+V5rWL6K+V?=",
        mailboxAddress: "alpha@shiro.local",
        fromAddr: "=?UTF-8?B?5pyo5YG2?= <ops@example.com>",
        status: "new",
        receivedAt: "2026-04-03T10:00:00Z",
      },
      {
        id: 2,
        subject: "Weekly summary",
        mailboxAddress: "beta@shiro.local",
        fromAddr: "digest@example.com",
        status: "seen",
        receivedAt: "2026-04-03T09:00:00Z",
      },
    ]);
  });

  it("renders message feed items", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminMessagesPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("测试测试")).toBeInTheDocument();
    expect(await screen.findByText("Weekly summary")).toBeInTheDocument();
    expect(await screen.findByText("木偶 <ops@example.com> → alpha@shiro.local")).toBeInTheDocument();
  });

  it("filters messages by keyword and status", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminMessagesPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    await screen.findByText("测试测试");

    fireEvent.change(screen.getByPlaceholderText("搜索主题 / 发件人 / 收件邮箱"), {
      target: { value: "digest" },
    });
    expect(screen.getByText("Weekly summary")).toBeInTheDocument();
    expect(screen.queryByText("测试测试")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("combobox", { name: "消息状态" }));
    fireEvent.click(await screen.findByRole("option", { name: "new" }));

    expect(screen.queryByText("测试测试")).not.toBeInTheDocument();
    expect(screen.queryByText("Weekly summary")).not.toBeInTheDocument();
  });
});
