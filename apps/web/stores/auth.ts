import { defineStore } from "pinia";
import { ref, computed } from "vue";

const TOKEN_KEY = "daas_token";

export interface JwtPayload {
  sub: string;
  email: string;
  exp: number;
  app_metadata: {
    tenant_id: string;
    role: "owner" | "employee" | "rider";
  };
}

export interface User {
  id: string;
  email: string;
  tenantId: string;
  role: "owner" | "employee" | "rider";
}

export interface Tenant {
  id: string;
  name: string;
  countryCode: string;
  fiscalId?: string;
  slug: string;
  features: { storefront: boolean; fiscal_invoicing: boolean };
  deliveryEnabled: boolean;
}

export interface RegisterPayload {
  name: string;
  email: string;
  password: string;
  country_code: string;
  fiscal_id?: string;
}

function decodeJwt(token: string): JwtPayload | null {
  try {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const payload = parts[1];
    const padded = payload + "=".repeat((4 - (payload.length % 4)) % 4);
    const decoded = atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
    return JSON.parse(decoded) as JwtPayload;
  } catch {
    return null;
  }
}

function isTokenExpired(payload: JwtPayload): boolean {
  return Date.now() >= payload.exp * 1000;
}

export const useAuthStore = defineStore("auth", () => {
  const accessToken = ref<string | null>(null);
  const user = ref<User | null>(null);
  const tenant = ref<Tenant | null>(null);

  const isAuthenticated = computed(() => {
    if (!accessToken.value || !user.value) return false;
    const payload = decodeJwt(accessToken.value);
    if (!payload) return false;
    return !isTokenExpired(payload);
  });

  const isOwner = computed(() => user.value?.role === "owner");
  const isRider = computed(() => user.value?.role === "rider");

  const tenantId = computed(() => user.value?.tenantId ?? null);

  function _hydrateFromToken(token: string): boolean {
    const payload = decodeJwt(token);
    if (!payload || isTokenExpired(payload)) {
      return false;
    }
    accessToken.value = token;
    user.value = {
      id: payload.sub,
      email: payload.email,
      tenantId: payload.app_metadata.tenant_id,
      role: payload.app_metadata.role,
    };
    return true;
  }

  function initFromStorage(): void {
    if (import.meta.server) return;
    const stored = localStorage.getItem(TOKEN_KEY);
    if (stored) {
      if (!_hydrateFromToken(stored)) {
        localStorage.removeItem(TOKEN_KEY);
      }
    }
  }

  async function fetchTenant(): Promise<void> {
    if (!accessToken.value) return;
    try {
      const config = useRuntimeConfig();
      const data = await $fetch<{
        id: string;
        name: string;
        country_code: string;
        fiscal_id?: string;
        slug: string;
        features?: { storefront: boolean; fiscal_invoicing: boolean };
        delivery_enabled?: boolean;
      }>(`${config.public.apiBase}/api/v1/tenants/me`, {
        headers: { Authorization: `Bearer ${accessToken.value}` },
      });
      tenant.value = {
        id: data.id,
        name: data.name,
        countryCode: data.country_code,
        fiscalId: data.fiscal_id,
        slug: data.slug,
        features: data.features ?? {
          storefront: false,
          fiscal_invoicing: false,
        },
        deliveryEnabled: data.delivery_enabled === true,
      };
    } catch (err: unknown) {
      const status =
        (err as { response?: { status?: number } })?.response?.status ??
        (err as { status?: number })?.status;
      if (status === 401) {
        // Token rejected by backend — clear stale auth state
        accessToken.value = null;
        user.value = null;
        tenant.value = null;
        if (!import.meta.server) {
          localStorage.removeItem(TOKEN_KEY);
        }
      }
      // Other errors are non-fatal — UI degrades gracefully without tenant name
    }
  }

  async function login(email: string, password: string): Promise<void> {
    const config = useRuntimeConfig();
    const data = await $fetch<{
      access_token: string;
      token_type: string;
      expires_in: number;
      refresh_token: string;
      user: { id: string; email: string };
    }>(`${config.public.apiBase}/api/v1/auth/login`, {
      method: "POST",
      body: { email, password },
    });

    if (!import.meta.server) {
      localStorage.setItem(TOKEN_KEY, data.access_token);
    }
    _hydrateFromToken(data.access_token);
    await fetchTenant();
  }

  async function logout(): Promise<void> {
    try {
      if (accessToken.value) {
        const config = useRuntimeConfig();
        await $fetch(`${config.public.apiBase}/api/v1/auth/logout`, {
          method: "POST",
          headers: { Authorization: `Bearer ${accessToken.value}` },
        });
      }
    } catch {
      // best-effort logout
    } finally {
      accessToken.value = null;
      user.value = null;
      tenant.value = null;
      if (!import.meta.server) {
        localStorage.removeItem(TOKEN_KEY);
      }
    }
  }

  async function register(payload: RegisterPayload): Promise<void> {
    const config = useRuntimeConfig();
    await $fetch<{ tenant_id: string; user_id: string; email: string }>(
      `${config.public.apiBase}/api/v1/auth/register`,
      {
        method: "POST",
        body: payload,
      },
    );
    await login(payload.email, payload.password);
  }

  // Sends a password-recovery email via Supabase. redirect_to points back to
  // the app's /auth/update-password page; Supabase validates it against its
  // allow list. Always resolves — the API never reveals if the email exists.
  async function requestPasswordReset(email: string): Promise<void> {
    const config = useRuntimeConfig();
    const redirectTo = import.meta.client
      ? `${window.location.origin}/auth/update-password`
      : "";
    await $fetch(`${config.public.apiBase}/api/v1/auth/recover`, {
      method: "POST",
      body: { email, redirect_to: redirectTo },
    });
  }

  // Sets a new password using the recovery access_token from the email link.
  async function updatePassword(
    accessToken: string,
    password: string,
  ): Promise<void> {
    const config = useRuntimeConfig();
    await $fetch(`${config.public.apiBase}/api/v1/auth/password`, {
      method: "PUT",
      headers: { Authorization: `Bearer ${accessToken}` },
      body: { password },
    });
  }

  function setTenant(data: Tenant): void {
    tenant.value = data;
  }

  return {
    accessToken,
    user,
    tenant,
    isAuthenticated,
    isOwner,
    isRider,
    tenantId,
    login,
    logout,
    register,
    requestPasswordReset,
    updatePassword,
    setTenant,
    fetchTenant,
    initFromStorage,
  };
});
