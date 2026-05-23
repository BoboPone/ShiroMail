import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  deleteAdminDomain,
  fetchAdminDomainProviders,
  fetchAdminDomains,
  fetchAdminSettingsSections,
  generateAdminSubdomains,
  reviewAdminDomainPublication,
  upsertAdminDomain,
  verifyAdminDomain,
} from "../api";
import { AdminDomainsPage } from "./domains-page";

vi.mock("../api", () => ({
  fetchAdminDomains: vi.fn(),
  fetchAdminDomainProviders: vi.fn(),
  fetchAdminSettingsSections: vi.fn(),
  upsertAdminDomain: vi.fn(),
  generateAdminSubdomains: vi.fn(),
  deleteAdminDomain: vi.fn(),
  reviewAdminDomainPublication: vi.fn(),
  verifyAdminDomain: vi.fn(),
}));

function renderPage(queryClient: QueryClient) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <AdminDomainsPage />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("AdminDomainsPage", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();

    vi.mocked(fetchAdminSettingsSections).mockResolvedValue([
      {
        key: "mail",
        title: "Mail",
        description: "",
        items: [
          {
            key: "mail.smtp",
            value: {
              enabled: true,
              listenAddr: ":2525",
              hostname: "mail.mmjs.top",
              dkimCnameTarget: "shiro._domainkey.mmjs.top",
              maxMessageBytes: 10485760,
            },
            updatedBy: 1,
            updatedAt: "2026-04-03T10:00:00Z",
          },
        ],
      },
    ]);

    vi.mocked(fetchAdminDomainProviders).mockResolvedValue([
      {
        id: 1,
        provider: "cloudflare",
        ownerType: "platform",
        displayName: "Primary Cloudflare",
        authType: "api_token",
        hasSecret: true,
        status: "healthy",
        capabilities: ["zones.read", "dns.write"],
        createdAt: "2026-04-03T10:00:00Z",
        updatedAt: "2026-04-03T10:00:00Z",
      },
    ]);

    vi.mocked(fetchAdminDomains).mockResolvedValue([
      {
        id: 7,
        domain: "provider-bound.test",
        status: "active",
        ownerUserId: 3,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 100,
        healthStatus: "healthy",
        providerAccountId: 1,
        provider: "cloudflare",
        providerDisplayName: "Primary Cloudflare",
        isDefault: false,
        weight: 100,
        rootDomain: "provider-bound.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
      {
        id: 8,
        domain: "pending-review.test",
        status: "active",
        ownerUserId: 9,
        visibility: "public_pool",
        publicationStatus: "pending_review",
        verificationScore: 0,
        healthStatus: "unknown",
        isDefault: false,
        weight: 100,
        rootDomain: "pending-review.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
    ]);

    vi.mocked(upsertAdminDomain).mockResolvedValue({
      id: 7,
      domain: "mail.ops.test",
      status: "active",
      ownerUserId: 3,
      visibility: "private",
      publicationStatus: "draft",
      verificationScore: 0,
      healthStatus: "unknown",
      providerAccountId: 1,
      provider: "cloudflare",
      providerDisplayName: "Primary Cloudflare",
      isDefault: false,
      weight: 100,
      rootDomain: "mail.ops.test",
      parentDomain: "",
      level: 0,
      kind: "root",
    });
    vi.mocked(generateAdminSubdomains).mockResolvedValue([]);
    vi.mocked(deleteAdminDomain).mockResolvedValue({ ok: true });
    vi.mocked(reviewAdminDomainPublication).mockResolvedValue({
      id: 8,
      domain: "pending-review.test",
      status: "active",
      ownerUserId: 9,
      visibility: "public_pool",
      publicationStatus: "approved",
      verificationScore: 0,
      healthStatus: "healthy",
      isDefault: false,
      weight: 100,
      rootDomain: "pending-review.test",
      parentDomain: "",
      level: 0,
      kind: "root",
    });
    vi.mocked(verifyAdminDomain).mockResolvedValue({
      domain: {
        id: 7,
        domain: "provider-bound.test",
        status: "active",
        ownerUserId: 3,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 50,
        healthStatus: "degraded",
        providerAccountId: 1,
        provider: "cloudflare",
        providerDisplayName: "Primary Cloudflare",
        isDefault: false,
        weight: 100,
        rootDomain: "provider-bound.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
      passed: false,
      summary: "DNS 传播验证未通过，请根据缺失或漂移记录继续修复。",
      zoneName: "provider-bound.test",
      verifiedCount: 1,
      totalCount: 2,
      profiles: [
        {
          verificationType: "dmarc",
          status: "drifted",
          summary: "DMARC 仍未对齐",
          expectedRecords: [],
          observedRecords: [],
          repairRecords: [
            {
              type: "TXT",
              name: "_dmarc.provider-bound.test",
              value: "v=DMARC1; p=quarantine",
              ttl: 300,
              priority: 0,
              proxied: false,
            },
          ],
        },
      ],
    });
  });

  function createQueryClient() {
    return new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
  }

  it("renders current domain-management overview", async () => {
    renderPage(createQueryClient());

    expect(await screen.findByText("域名管理")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "添加域名" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "新增子域名" })).toBeInTheDocument();
    expect(await screen.findByText("域名资产")).toBeInTheDocument();
    expect((await screen.findAllByText("provider-bound.test")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("Primary Cloudflare")).length).toBeGreaterThan(0);
  });

  it("creates multilevel domains from add-domain dialog", async () => {
    renderPage(createQueryClient());

    fireEvent.click(await screen.findByRole("button", { name: "添加域名" }));

    const dialog = await screen.findByRole("dialog", { name: "添加域名" });
    const dialogQueries = within(dialog);
    fireEvent.change(dialogQueries.getByPlaceholderText("example.com"), {
      target: { value: "oom.fnrry.com" },
    });
    fireEvent.click(dialogQueries.getByRole("button", { name: "添加" }));

    await waitFor(() => {
      expect(vi.mocked(upsertAdminDomain).mock.calls[0]?.[0]).toMatchObject({
        domain: "oom.fnrry.com",
        status: "active",
        visibility: "private",
        publicationStatus: "draft",
        healthStatus: "unknown",
        weight: 100,
      });
    });
  });

  it("creates subdomains from dialog", async () => {
    renderPage(createQueryClient());

    fireEvent.click(screen.getByRole("button", { name: "新增子域名" }));

    const subdomainDialog = await screen.findByRole("dialog", { name: "批量生成子域名" });
    const subdomainDialogQueries = within(subdomainDialog);

    fireEvent.click(subdomainDialogQueries.getByRole("combobox", { name: "选择根域名" }));
    fireEvent.click(await screen.findByRole("option", { name: "provider-bound.test" }));
    fireEvent.change(subdomainDialogQueries.getByRole("textbox", { name: "多级前缀" }), {
      target: { value: "mx\nrelay" },
    });
    fireEvent.click(subdomainDialogQueries.getByRole("button", { name: "批量生成子域名" }));

    await waitFor(() => {
      expect(vi.mocked(generateAdminSubdomains).mock.calls[0]?.[0]).toEqual({
        baseDomainId: 7,
        prefixes: ["mx", "relay"],
        status: "active",
        visibility: "private",
        publicationStatus: "draft",
        healthStatus: "unknown",
        weight: 90,
      });
    });
  });

  it("triggers approval workflow for pending-review domains", async () => {
    renderPage(createQueryClient());

    fireEvent.click(await screen.findByRole("button", { name: "批准" }));

    await waitFor(() => {
      expect(vi.mocked(reviewAdminDomainPublication).mock.calls[0]?.[0]).toBe(8);
      expect(vi.mocked(reviewAdminDomainPublication).mock.calls[0]?.[1]).toBe("approve");
    });
    expect(await screen.findByText("域名已批准进入公共域名池。")).toBeInTheDocument();
  });

});
