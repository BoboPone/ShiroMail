import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  createAdminApiKey,
  fetchAdminApiKeys,
  fetchAdminDomains,
  revokeAdminApiKey,
  rotateAdminApiKey,
} from "../api";
import { AdminApiKeysPage } from "./api-keys-page";

vi.mock("../api", () => ({
  fetchAdminApiKeys: vi.fn(),
  fetchAdminDomains: vi.fn(),
  createAdminApiKey: vi.fn(),
  rotateAdminApiKey: vi.fn(),
  revokeAdminApiKey: vi.fn(),
}));

describe("AdminApiKeysPage", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();

    vi.mocked(fetchAdminApiKeys).mockResolvedValue([
      {
        id: 1,
        userId: 7,
        name: "worker",
        keyPrefix: "sk_live",
        keyPreview: "sk_live_preview",
        status: "active",
        createdAt: "2026-04-03T10:00:00Z",
        scopes: ["mailboxes.read", "domains.verify"],
        resourcePolicy: {
          domainAccessMode: "private_only",
          allowPlatformPublicDomains: false,
          allowUserPublishedDomains: false,
          allowOwnedPrivateDomains: true,
          allowProviderMutation: false,
          allowProtectedRecordWrite: false,
        },
        domainBindings: null as unknown as [],
      },
    ]);

    vi.mocked(fetchAdminDomains).mockResolvedValue([
      {
        id: 42,
        domain: "private-zone.test",
        status: "active",
        ownerUserId: 7,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 0,
        healthStatus: "healthy",
        isDefault: false,
        weight: 100,
        rootDomain: "private-zone.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
    ]);

    vi.mocked(createAdminApiKey).mockResolvedValue({
      id: 2,
      userId: 9,
      name: "ops-bot",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_new..._new",
      plainSecret: "sk_live_new",
      status: "active",
      createdAt: "2026-04-03T11:00:00Z",
      scopes: ["domains.read", "mailboxes.read", "messages.read", "domains.verify"],
      resourcePolicy: {
        domainAccessMode: "mixed",
        allowPlatformPublicDomains: true,
        allowUserPublishedDomains: true,
        allowOwnedPrivateDomains: true,
        allowProviderMutation: false,
        allowProtectedRecordWrite: false,
      },
      domainBindings: [],
    });
    vi.mocked(rotateAdminApiKey).mockResolvedValue({
      id: 1,
      userId: 7,
      name: "worker",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_rota...ated",
      plainSecret: "sk_live_rotated",
      status: "active",
      createdAt: "2026-04-03T10:00:00Z",
      rotatedAt: "2026-04-03T11:10:00Z",
      scopes: ["mailboxes.read", "domains.verify"],
      resourcePolicy: {
        domainAccessMode: "private_only",
        allowPlatformPublicDomains: false,
        allowUserPublishedDomains: false,
        allowOwnedPrivateDomains: true,
        allowProviderMutation: false,
        allowProtectedRecordWrite: false,
      },
      domainBindings: [
        {
          id: 11,
          nodeId: 42,
          accessLevel: "verify",
        },
      ],
    });
    vi.mocked(revokeAdminApiKey).mockResolvedValue({
      id: 1,
      userId: 7,
      name: "worker",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_rotated",
      status: "revoked",
      createdAt: "2026-04-03T10:00:00Z",
      revokedAt: "2026-04-03T11:11:00Z",
      scopes: ["mailboxes.read", "domains.verify"],
      resourcePolicy: {
        domainAccessMode: "private_only",
        allowPlatformPublicDomains: false,
        allowUserPublishedDomains: false,
        allowOwnedPrivateDomains: true,
        allowProviderMutation: false,
        allowProtectedRecordWrite: false,
      },
      domainBindings: [
        {
          id: 11,
          nodeId: 42,
          accessLevel: "verify",
        },
      ],
    });
  });

  it("renders enterprise api key policy details for admins", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("worker")).toBeInTheDocument();
    expect((await screen.findAllByText("mailboxes.read")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("domains.verify")).length).toBeGreaterThan(0);
    expect(await screen.findByText("private_only")).toBeInTheDocument();
    expect(await screen.findByText("绑定 0")).toBeInTheDocument();
  });

  it("creates admin api keys inside a dialog", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByPlaceholderText("输入用户 ID")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "新增 API Key" }));

    const dialog = await screen.findByRole("dialog", { name: "新增 API Key" });
    const dialogQueries = within(dialog);

    fireEvent.change(
      dialogQueries.getByPlaceholderText("输入密钥名称，如 Admin Worker / Audit Bot"),
      {
        target: { value: "ops-bot" },
      },
    );

    fireEvent.click(dialogQueries.getByRole("button", { name: "创建密钥" }));

    await waitFor(() => {
      expect(vi.mocked(createAdminApiKey).mock.calls[0]?.[0]).toEqual({
        name: "ops-bot",
        scopes: ["domains.read", "domains.verify", "mailboxes.read", "messages.read"],
        resourcePolicy: {
          domainAccessMode: "mixed",
          allowPlatformPublicDomains: true,
          allowUserPublishedDomains: true,
          allowOwnedPrivateDomains: true,
          allowProviderMutation: false,
          allowProtectedRecordWrite: false,
        },
        domainBindings: [],
      });
    });

    await waitFor(() => {
      expect(vi.mocked(createAdminApiKey).mock.calls).toHaveLength(1);
    });
  });

  it("rotates and revokes admin api keys from the list", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "轮换" }));
    await waitFor(() => {
      expect(vi.mocked(rotateAdminApiKey).mock.calls[0]?.[0]).toBe(1);
    });

    const revealDialog = await screen.findByRole("dialog", { name: "已轮换 API 密钥" });
    expect(revealDialog).toBeInTheDocument();
    expect(await screen.findByDisplayValue("sk_live_rotated")).toBeInTheDocument();
    fireEvent.click(within(revealDialog).getByRole("button", { name: "关闭" }));

    fireEvent.click(await screen.findByRole("button", { name: "撤销" }));
    expect(await screen.findByRole("dialog", { name: "确认撤销 API 密钥" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "确认撤销" }));

    await waitFor(() => {
      expect(vi.mocked(revokeAdminApiKey).mock.calls[0]?.[0]).toBe(1);
    });
  });

  it("shows a dialog error when api key creation fails", async () => {
    vi.mocked(createAdminApiKey).mockRejectedValueOnce(new Error("invalid scope policy"));

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新增 API Key" }));

    const dialog = await screen.findByRole("dialog", { name: "新增 API Key" });
    const dialogQueries = within(dialog);

    fireEvent.change(
      dialogQueries.getByPlaceholderText("输入密钥名称，如 Admin Worker / Audit Bot"),
      {
        target: { value: "ops-bot" },
      },
    );

    fireEvent.click(dialogQueries.getByRole("button", { name: "创建密钥" }));

    expect(await dialogQueries.findByText("invalid scope policy")).toBeInTheDocument();
  });

  it("only renders active keys and still shows revoke confirmation details", async () => {
    vi.mocked(fetchAdminApiKeys).mockResolvedValueOnce([
      {
        id: 1,
        userId: 7,
        name: "worker-active",
        keyPrefix: "sk_live",
        keyPreview: "sk_live_acti...1234",
        status: "active",
        createdAt: "2026-04-03T10:00:00Z",
        lastUsedAt: "2026-04-04T11:00:00Z",
        scopes: ["mailboxes.read"],
        resourcePolicy: {
          domainAccessMode: "mixed",
          allowPlatformPublicDomains: true,
          allowUserPublishedDomains: true,
          allowOwnedPrivateDomains: true,
          allowProviderMutation: false,
          allowProtectedRecordWrite: false,
        },
        domainBindings: [],
      },
    ]);

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <AdminApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("worker-active")).toBeInTheDocument();
    expect(screen.queryByText("worker-revoked")).not.toBeInTheDocument();
    fireEvent.click((await screen.findAllByRole("button", { name: "撤销" }))[0]);

    expect(await screen.findByRole("dialog", { name: "确认撤销 API 密钥" })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("worker-active")).toBeInTheDocument();
  });
});
