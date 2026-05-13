import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  createApiKey,
  fetchApiKeys,
  fetchDomains,
  revokeApiKey,
  rotateApiKey,
} from "../api";
import { UserApiKeysPage } from "./api-keys-page";

vi.mock("../api", () => ({
  fetchApiKeys: vi.fn(),
  fetchDomains: vi.fn(),
  createApiKey: vi.fn(),
  rotateApiKey: vi.fn(),
  revokeApiKey: vi.fn(),
}));

describe("UserApiKeysPage", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();

    vi.mocked(fetchDomains).mockResolvedValue([
      {
        id: 42,
        domain: "private.example.com",
        status: "active",
        ownerUserId: 7,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 100,
        healthStatus: "healthy",
        isDefault: false,
        weight: 100,
        rootDomain: "example.com",
        parentDomain: "example.com",
        level: 1,
        kind: "subdomain",
      },
      {
        id: 99,
        domain: "pool.example.com",
        status: "active",
        visibility: "public_pool",
        publicationStatus: "approved",
        verificationScore: 100,
        healthStatus: "healthy",
        isDefault: false,
        weight: 90,
        rootDomain: "example.com",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
    ]);

    vi.mocked(fetchApiKeys).mockResolvedValue([
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

    vi.mocked(createApiKey).mockResolvedValue({
      id: 2,
      userId: 7,
      name: "sdk",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_prev...ew_2",
      plainSecret: "sk_live_preview_2",
      status: "active",
      createdAt: "2026-04-03T10:00:00Z",
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
    });
    vi.mocked(rotateApiKey).mockResolvedValue({
      id: 1,
      userId: 7,
      name: "worker",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_rotated",
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
      domainBindings: [
        {
          id: 11,
          zoneId: 42,
          accessLevel: "verify",
        },
      ],
    });
    vi.mocked(revokeApiKey).mockResolvedValue({
      id: 1,
      userId: 7,
      name: "worker",
      keyPrefix: "sk_live",
      keyPreview: "sk_live_preview",
      status: "revoked",
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
      domainBindings: [
        {
          id: 11,
          zoneId: 42,
          accessLevel: "verify",
        },
      ],
    });
  });

  it("renders scope and policy summaries for enterprise api keys", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <UserApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("worker")).toBeInTheDocument();
    expect((await screen.findAllByText("mailboxes.read")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("domains.verify")).length).toBeGreaterThan(0);
    expect(await screen.findByText("private_only")).toBeInTheDocument();
    expect(await screen.findByText("绑定 0")).toBeInTheDocument();
  });

  it("creates enterprise api keys with selected scopes, policy and domain bindings", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <UserApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(
      screen.queryByPlaceholderText("输入密钥名称，如 SDK / Bot / Worker"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "创建密钥" }));

    const dialog = await screen.findByRole("dialog");
    const dialogQueries = within(dialog);

    fireEvent.change(
      dialogQueries.getByPlaceholderText("输入密钥名称，如 SDK / Bot / Worker"),
      {
      target: { value: "ops-key" },
      },
    );

    fireEvent.click(dialogQueries.getByRole("combobox", { name: "域访问模式" }));
    fireEvent.click((await screen.findAllByText("private_only")).at(-1)!);

    fireEvent.click(
      dialogQueries.getByRole("checkbox", { name: "provider.accounts.read" }),
    );
    fireEvent.click(dialogQueries.getByRole("checkbox", { name: "platform_public" }));
    fireEvent.click(dialogQueries.getByRole("checkbox", { name: "public_pool" }));

    fireEvent.click(dialogQueries.getByRole("combobox", { name: "绑定域名" }));
    fireEvent.click(await screen.findByText("private.example.com"));

    fireEvent.click(dialogQueries.getByRole("combobox", { name: "绑定权限" }));
    fireEvent.click((await screen.findAllByText("verify")).at(-1)!);

    fireEvent.click(dialogQueries.getByRole("button", { name: "添加绑定" }));
    fireEvent.click(dialogQueries.getByRole("button", { name: "创建密钥" }));

    await waitFor(() => {
      expect(vi.mocked(createApiKey).mock.calls[0]?.[0]).toEqual({
        name: "ops-key",
        scopes: [
          "domains.read",
          "domains.verify",
          "mailboxes.read",
          "messages.read",
          "provider.accounts.read",
        ],
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
            nodeId: 42,
            accessLevel: "verify",
          },
        ],
      });
    });

    expect(await screen.findByRole("dialog", { name: "API 密钥已创建" })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("sk_live_preview_2")).toBeInTheDocument();
  });

  it("hides revoked keys from the user list and asks for revoke confirmation", async () => {
    vi.mocked(fetchApiKeys).mockResolvedValueOnce([
      {
        id: 7,
        userId: 7,
        name: "active-worker",
        keyPrefix: "sk_live",
        keyPreview: "sk_live_acti...1234",
        status: "active",
        createdAt: "2026-04-03T10:00:00Z",
        lastUsedAt: "2026-04-04T10:00:00Z",
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
      {
        id: 8,
        userId: 7,
        name: "revoked-worker",
        keyPrefix: "sk_live",
        keyPreview: "sk_live_revo...5678",
        status: "revoked",
        createdAt: "2026-04-03T10:00:00Z",
        scopes: ["domains.read"],
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
          <UserApiKeysPage />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("active-worker")).toBeInTheDocument();
    expect(screen.queryByText("revoked-worker")).not.toBeInTheDocument();

    fireEvent.click((await screen.findAllByRole("button", { name: "撤销" }))[0]);

    expect(await screen.findByRole("dialog", { name: "确认撤销 API 密钥" })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("active-worker")).toBeInTheDocument();
  });
});
