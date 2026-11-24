<script setup lang="ts">
import {
  Settings,
  Users,
  Zap,
  Save,
  X,
  Loader2,
  Plus,
  Eye,
  EyeOff,
  Store,
  Bike,
  Copy,
  Check,
} from "lucide-vue-next";
import { useAuthStore } from "~/stores/auth";
import { useApiFetch } from "~/composables/useAuth";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

const store = useAuthStore();
const { isMobile } = useMobileMode();

async function handleLogout() {
  await store.logout();
  await navigateTo("/auth/login");
}
const activeTab = ref("empresa");

// ─── Tab 1: Empresa (tenant profile) ────────────────────────────────────────
const isEditingTenant = ref(false);
const savingTenant = ref(false);
const tenantName = ref(store.tenant?.name ?? "");
const tenantFiscalId = ref(store.tenant?.fiscalId ?? "");
const tenantCountry = ref(store.tenant?.countryCode ?? "");

function startEditTenant() {
  tenantName.value = store.tenant?.name ?? "";
  tenantFiscalId.value = store.tenant?.fiscalId ?? "";
  isEditingTenant.value = true;
}

function cancelEditTenant() {
  isEditingTenant.value = false;
}

async function saveTenant() {
  if (!tenantName.value.trim()) return;
  savingTenant.value = true;
  try {
    await useApiFetch("/api/v1/tenants/me", {
      method: "PATCH",
      body: {
        name: tenantName.value.trim(),
        fiscal_id: tenantFiscalId.value.trim(),
      },
    });
    await store.fetchTenant();
    isEditingTenant.value = false;
  } catch {
    // error shown via toast in future — silent for now
  } finally {
    savingTenant.value = false;
  }
}

// ─── Tab 1 (cont.): Funcionalidades (self-service feature toggles) ──────────
const storefrontEnabled = ref(store.tenant?.features?.storefront ?? false);
const deliveryEnabled = ref(store.tenant?.deliveryEnabled ?? false);
const savingFeature = ref<"storefront" | "delivery" | null>(null);

async function toggleFeature(
  feature: "storefront" | "delivery",
  value: boolean,
) {
  savingFeature.value = feature;
  try {
    await useApiFetch("/api/v1/tenants/me/features", {
      method: "PATCH",
      body: { [feature]: value },
    });
    if (feature === "storefront") {
      storefrontEnabled.value = value;
    } else {
      deliveryEnabled.value = value;
    }
    await store.fetchTenant();
  } catch {
    // revert the switch to its last known-good state on failure
    if (feature === "storefront") {
      storefrontEnabled.value = store.tenant?.features?.storefront ?? false;
    } else {
      deliveryEnabled.value = store.tenant?.deliveryEnabled ?? false;
    }
  } finally {
    savingFeature.value = null;
  }
}

// ─── Tab 2: Usuarios ─────────────────────────────────────────────────────────
interface UserRecord {
  id: string;
  tenant_id: string;
  email: string;
  role: "owner" | "employee" | "rider";
  active: boolean;
  created_at: string;
}

const users = ref<UserRecord[]>([]);
const loadingUsers = ref(false);
const togglingUserId = ref<string | null>(null);
const inviteDialogOpen = ref(false);
const inviteEmail = ref("");
const inviteDraftPassword = ref("");
const inviteRole = ref<"employee" | "rider">("employee");
const inviting = ref(false);
const inviteError = ref("");
const inviteSuccess = ref<{ email: string; password: string } | null>(null);
const showInvitePassword = ref(false);
const copiedInviteField = ref<"email" | "password" | null>(null);

async function copyInviteField(field: "email" | "password", value: string) {
  try {
    await navigator.clipboard.writeText(value);
    copiedInviteField.value = field;
    setTimeout(() => {
      if (copiedInviteField.value === field) copiedInviteField.value = null;
    }, 1500);
  } catch {
    // clipboard unavailable — ignore silently, value is still shown on screen
  }
}

async function fetchUsers() {
  loadingUsers.value = true;
  try {
    const data = await useApiFetch<UserRecord[]>("/api/v1/users");
    users.value = data;
  } catch {
    // handled silently — table shows empty state
  } finally {
    loadingUsers.value = false;
  }
}

async function toggleUserActive(user: UserRecord) {
  togglingUserId.value = user.id;
  try {
    await useApiFetch(`/api/v1/users/${user.id}`, {
      method: "PATCH",
      body: { active: !user.active },
    });
    user.active = !user.active;
  } catch {
    // revert optimistic state if needed
  } finally {
    togglingUserId.value = null;
  }
}

async function sendInvite() {
  if (!inviteEmail.value) {
    inviteError.value = "Ingresá un email válido";
    return;
  }
  if (inviteDraftPassword.value.length < 6) {
    inviteError.value = "La contraseña debe tener al menos 6 caracteres";
    return;
  }
  inviting.value = true;
  inviteError.value = "";
  try {
    await useApiFetch("/api/v1/users/invite", {
      method: "POST",
      body: {
        email: inviteEmail.value,
        password: inviteDraftPassword.value,
        role: inviteRole.value,
      },
    });
    inviteSuccess.value = {
      email: inviteEmail.value,
      password: inviteDraftPassword.value,
    };
    await fetchUsers();
  } catch (err: unknown) {
    const apiError = err as { data?: { detail?: string } };
    inviteError.value = apiError?.data?.detail ?? "Error al crear la cuenta";
  } finally {
    inviting.value = false;
  }
}

function closeInviteDialog() {
  inviteDialogOpen.value = false;
  inviteEmail.value = "";
  inviteDraftPassword.value = "";
  inviteRole.value = "employee";
  inviteError.value = "";
  inviteSuccess.value = null;
  showInvitePassword.value = false;
  copiedInviteField.value = null;
}

// ─── Tab 3: Integraciones ────────────────────────────────────────────────────
interface FiscalIntegration {
  active: boolean;
  configured: boolean;
  api_key_masked?: string;
}

const fiscalIntegration = ref<FiscalIntegration | null>(null);
const loadingFiscal = ref(false);
const apiKey = ref("");
const showApiKey = ref(false);
const savingFiscal = ref(false);
const fiscalSaveError = ref("");
const fiscalSaveSuccess = ref(false);

async function fetchFiscalIntegration() {
  loadingFiscal.value = true;
  try {
    const data = await useApiFetch<FiscalIntegration>(
      "/api/v1/tenant/integrations/fiscal",
    );
    fiscalIntegration.value = data;
  } catch {
    // not configured yet
  } finally {
    loadingFiscal.value = false;
  }
}

async function saveFiscalKey() {
  if (!apiKey.value.trim()) {
    fiscalSaveError.value = "Ingresá la clave de API";
    return;
  }
  savingFiscal.value = true;
  fiscalSaveError.value = "";
  fiscalSaveSuccess.value = false;
  try {
    await useApiFetch("/api/v1/tenant/integrations/fiscal", {
      method: "PUT",
      body: { api_key: apiKey.value },
    });
    fiscalSaveSuccess.value = true;
    apiKey.value = "";
    await fetchFiscalIntegration();
  } catch (err: unknown) {
    const apiError = err as { data?: { detail?: string } };
    fiscalSaveError.value =
      apiError?.data?.detail ?? "Error al guardar la clave";
  } finally {
    savingFiscal.value = false;
  }
}

// Load data when tabs change
watch(
  activeTab,
  async (tab) => {
    if (tab === "usuarios" && users.value.length === 0) await fetchUsers();
    if (tab === "integraciones") await fetchFiscalIntegration();
  },
  { immediate: false },
);

// Load users tab if starting there
onMounted(() => {
  if (activeTab.value === "usuarios") fetchUsers();
  if (activeTab.value === "integraciones") fetchFiscalIntegration();
});
</script>

<template>
  <div>
    <!-- MOBILE -->
    <MobileScreensSettings
      v-if="isMobile"
      :tenant-name="store.tenant?.name ?? ''"
      :user-email="store.user?.email ?? ''"
      :is-owner="store.isOwner"
      @logout="handleLogout"
    />

    <!-- WEB -->
    <div v-else>
      <!-- Header -->
      <div class="mb-8 flex items-center gap-3">
        <div
          class="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center"
        >
          <Settings class="w-5 h-5 text-primary" />
        </div>
        <div>
          <h1 class="text-2xl font-bold font-heading text-foreground">
            Configuración
          </h1>
          <p class="text-muted-foreground text-sm">
            Gestioná tu empresa, usuarios e integraciones
          </p>
        </div>
      </div>

      <!-- Tabs nav -->
      <div class="flex border-b gap-1 mb-6">
        <button
          v-for="tab in [
            { value: 'empresa', label: 'Empresa', icon: Settings },
            { value: 'usuarios', label: 'Usuarios', icon: Users },
            { value: 'integraciones', label: 'Integraciones', icon: Zap },
          ]"
          :key="tab.value"
          type="button"
          :class="[
            'flex items-center gap-2 px-4 py-2.5 text-sm font-semibold transition-all duration-200 cursor-pointer border-b-2 -mb-px',
            activeTab === tab.value
              ? 'border-primary text-primary'
              : 'border-transparent text-muted-foreground hover:text-primary hover:border-primary/40',
          ]"
          @click="activeTab = tab.value"
        >
          <component :is="tab.icon" class="w-4 h-4" />
          {{ tab.label }}
        </button>
      </div>

      <!-- Tab: Empresa -->
      <div v-if="activeTab === 'empresa'">
        <div class="rounded-xl border bg-card shadow-sm max-w-2xl">
          <div class="px-6 py-5 border-b flex items-center justify-between">
            <h2 class="text-lg font-bold font-heading text-foreground">
              Perfil de empresa
            </h2>
            <button
              v-if="!isEditingTenant"
              type="button"
              class="flex items-center gap-2 h-9 px-4 text-sm font-semibold text-primary border border-primary rounded-lg hover:bg-primary/5 transition-all duration-200 cursor-pointer"
              @click="startEditTenant"
            >
              Editar
            </button>
          </div>

          <div class="px-6 py-5 space-y-4">
            <template v-if="!isEditingTenant">
              <!-- Read view -->
              <div class="grid grid-cols-2 gap-4">
                <div>
                  <p
                    class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
                  >
                    Empresa
                  </p>
                  <p class="text-base font-medium text-foreground">
                    {{ store.tenant?.name || "—" }}
                  </p>
                </div>
                <div>
                  <p
                    class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
                  >
                    Pais
                  </p>
                  <p class="text-base font-medium text-foreground">
                    {{ store.tenant?.countryCode || "—" }}
                  </p>
                </div>
                <div>
                  <p
                    class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
                  >
                    ID Fiscal
                  </p>
                  <p class="text-base font-medium text-foreground">
                    {{ store.tenant?.fiscalId || "—" }}
                  </p>
                </div>
              </div>
            </template>

            <template v-else>
              <!-- Edit view -->
              <div class="space-y-4">
                <div>
                  <label
                    for="edit-name"
                    class="block text-sm font-semibold text-foreground mb-1.5"
                  >
                    Nombre de la empresa
                  </label>
                  <input
                    id="edit-name"
                    v-model="tenantName"
                    type="text"
                    class="w-full h-11 px-4 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 bg-white"
                  />
                </div>
                <div>
                  <label
                    for="edit-fiscal"
                    class="block text-sm font-semibold text-foreground mb-1.5"
                  >
                    ID Fiscal
                  </label>
                  <input
                    id="edit-fiscal"
                    v-model="tenantFiscalId"
                    type="text"
                    class="w-full h-11 px-4 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 bg-white"
                  />
                </div>

                <div class="flex gap-3 pt-2">
                  <button
                    type="button"
                    :disabled="savingTenant"
                    class="flex items-center gap-2 h-10 px-5 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
                    @click="saveTenant"
                  >
                    <Loader2 v-if="savingTenant" class="w-4 h-4 animate-spin" />
                    <Save v-else class="w-4 h-4" />
                    Guardar
                  </button>
                  <button
                    type="button"
                    class="flex items-center gap-2 h-10 px-4 border text-muted-foreground text-sm font-semibold rounded-lg hover:bg-muted transition-all duration-200 cursor-pointer"
                    @click="cancelEditTenant"
                  >
                    <X class="w-4 h-4" />
                    Cancelar
                  </button>
                </div>
              </div>
            </template>
          </div>
        </div>

        <!-- Funcionalidades (self-service feature toggles) -->
        <div class="rounded-xl border bg-card shadow-sm max-w-2xl mt-6">
          <div class="px-6 py-5 border-b">
            <h2 class="text-lg font-bold font-heading text-foreground">
              Funcionalidades
            </h2>
            <p class="text-xs text-muted-foreground mt-1">
              Habilitá o deshabilitá funcionalidades para tu empresa
            </p>
          </div>

          <div class="px-6 py-5 space-y-5">
            <div class="flex items-center justify-between gap-4">
              <div class="flex items-center gap-3">
                <div
                  class="w-9 h-9 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0"
                >
                  <Store class="w-5 h-5 text-primary" />
                </div>
                <div>
                  <p class="text-sm font-semibold text-foreground">
                    Tienda online
                  </p>
                  <p class="text-xs text-muted-foreground">
                    Habilita el catálogo público y pedidos online en /t/{{
                      store.tenant?.slug ?? "tu-slug"
                    }}
                  </p>
                </div>
              </div>
              <Switch
                :model-value="storefrontEnabled"
                :disabled="savingFeature === 'storefront'"
                class="flex-shrink-0"
                @update:model-value="toggleFeature('storefront', $event)"
              />
            </div>

            <div class="flex items-center justify-between gap-4">
              <div class="flex items-center gap-3">
                <div
                  class="w-9 h-9 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0"
                >
                  <Bike class="w-5 h-5 text-primary" />
                </div>
                <div>
                  <p class="text-sm font-semibold text-foreground">
                    Delivery / Motorizados
                  </p>
                  <p class="text-xs text-muted-foreground">
                    Habilita la gestión de motorizados y entregas a domicilio
                  </p>
                </div>
              </div>
              <Switch
                :model-value="deliveryEnabled"
                :disabled="savingFeature === 'delivery'"
                class="flex-shrink-0"
                @update:model-value="toggleFeature('delivery', $event)"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Tab: Usuarios -->
      <div v-if="activeTab === 'usuarios'">
        <div class="rounded-xl border bg-card shadow-sm">
          <div class="px-6 py-5 border-b flex items-center justify-between">
            <h2 class="text-lg font-bold font-heading text-foreground">
              Usuarios del tenant
            </h2>
            <button
              v-if="store.isOwner"
              type="button"
              class="flex items-center gap-2 h-10 px-4 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer"
              @click="inviteDialogOpen = true"
            >
              <Plus class="w-4 h-4" />
              Crear usuario
            </button>
          </div>

          <!-- Loading skeleton -->
          <div v-if="loadingUsers" class="px-6 py-5 space-y-3">
            <div
              v-for="n in 3"
              :key="n"
              class="h-12 animate-pulse bg-muted rounded-lg"
            />
          </div>

          <!-- Users table -->
          <div v-else-if="users.length > 0" class="overflow-x-auto">
            <table class="w-full">
              <thead>
                <tr class="border-b">
                  <th
                    class="px-6 py-3 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wide"
                  >
                    Email
                  </th>
                  <th
                    class="px-6 py-3 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wide"
                  >
                    Rol
                  </th>
                  <th
                    class="px-6 py-3 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wide"
                  >
                    Estado
                  </th>
                  <th
                    v-if="store.isOwner"
                    class="px-6 py-3 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wide"
                  >
                    Acciones
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                <tr
                  v-for="u in users"
                  :key="u.id"
                  class="hover:bg-muted/50 transition-colors duration-150"
                >
                  <td class="px-6 py-4 text-sm font-medium text-foreground">
                    {{ u.email }}
                  </td>
                  <td class="px-6 py-4">
                    <span
                      :class="[
                        'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold',
                        u.role === 'owner'
                          ? 'bg-primary/10 text-primary'
                          : 'bg-muted text-muted-foreground',
                      ]"
                    >
                      {{
                        u.role === "owner"
                          ? "Propietario"
                          : u.role === "rider"
                            ? "Repartidor"
                            : "Empleado"
                      }}
                    </span>
                  </td>
                  <td class="px-6 py-4">
                    <span
                      :class="[
                        'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold',
                        u.active
                          ? 'bg-green-100 text-green-700'
                          : 'bg-muted text-muted-foreground',
                      ]"
                    >
                      {{ u.active ? "Activo" : "Inactivo" }}
                    </span>
                  </td>
                  <td v-if="store.isOwner" class="px-6 py-4">
                    <div class="flex items-center gap-2">
                      <button
                        type="button"
                        :disabled="
                          togglingUserId === u.id || u.role === 'owner'
                        "
                        :title="u.active ? 'Desactivar' : 'Activar'"
                        :class="[
                          'relative inline-flex h-6 w-11 items-center rounded-full transition-colors duration-200 cursor-pointer',
                          u.active ? 'bg-primary' : 'bg-muted',
                          (togglingUserId === u.id || u.role === 'owner') &&
                            'opacity-50 cursor-not-allowed',
                        ]"
                        role="switch"
                        :aria-checked="u.active"
                        @click="toggleUserActive(u)"
                      >
                        <span
                          :class="[
                            'inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200',
                            u.active ? 'translate-x-6' : 'translate-x-1',
                          ]"
                        />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Empty state -->
          <div v-else class="px-6 py-12 text-center">
            <Users class="w-10 h-10 text-muted-foreground/20 mx-auto mb-3" />
            <p class="text-muted-foreground font-medium">
              No hay usuarios registrados
            </p>
          </div>
        </div>

        <!-- Invite dialog -->
        <Teleport to="body">
          <Transition
            enter-active-class="transition-opacity duration-200"
            enter-from-class="opacity-0"
            enter-to-class="opacity-100"
            leave-active-class="transition-opacity duration-200"
            leave-from-class="opacity-100"
            leave-to-class="opacity-0"
          >
            <div
              v-if="inviteDialogOpen"
              class="fixed inset-0 z-50 flex items-center justify-center p-4"
            >
              <div
                class="absolute inset-0 bg-black/50"
                @click="closeInviteDialog"
              />
              <div
                class="relative z-10 w-full max-w-md bg-white rounded-xl shadow-xl p-6"
              >
                <h3 class="text-xl font-bold font-heading text-foreground mb-4">
                  {{ inviteSuccess ? "Cuenta creada" : "Crear usuario" }}
                </h3>

                <!-- Success confirmation panel -->
                <div v-if="inviteSuccess" class="space-y-4">
                  <p class="text-sm text-muted-foreground">
                    La cuenta se creó correctamente. Compartí estas credenciales
                    con el usuario; la contraseña no va a volver a mostrarse.
                  </p>

                  <div>
                    <label
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      Email
                    </label>
                    <div
                      class="flex items-center gap-2 h-11 px-4 border rounded-lg bg-muted/40"
                    >
                      <span class="flex-1 truncate text-base">{{
                        inviteSuccess.email
                      }}</span>
                      <button
                        type="button"
                        class="text-muted-foreground hover:text-foreground cursor-pointer"
                        @click="copyInviteField('email', inviteSuccess.email)"
                      >
                        <Check
                          v-if="copiedInviteField === 'email'"
                          class="w-4 h-4 text-green-600"
                        />
                        <Copy v-else class="w-4 h-4" />
                      </button>
                    </div>
                  </div>

                  <div>
                    <label
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      Contraseña
                    </label>
                    <div
                      class="flex items-center gap-2 h-11 px-4 border rounded-lg bg-muted/40"
                    >
                      <span class="flex-1 truncate text-base font-mono">{{
                        showInvitePassword
                          ? inviteSuccess.password
                          : "•".repeat(inviteSuccess.password.length)
                      }}</span>
                      <button
                        type="button"
                        class="text-muted-foreground hover:text-foreground cursor-pointer"
                        @click="showInvitePassword = !showInvitePassword"
                      >
                        <component
                          :is="showInvitePassword ? EyeOff : Eye"
                          class="w-4 h-4"
                        />
                      </button>
                      <button
                        type="button"
                        class="text-muted-foreground hover:text-foreground cursor-pointer"
                        @click="
                          copyInviteField('password', inviteSuccess.password)
                        "
                      >
                        <Check
                          v-if="copiedInviteField === 'password'"
                          class="w-4 h-4 text-green-600"
                        />
                        <Copy v-else class="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                </div>

                <!-- Create user form -->
                <div v-else class="space-y-4">
                  <div>
                    <label
                      for="invite-email"
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      Email
                    </label>
                    <input
                      id="invite-email"
                      v-model="inviteEmail"
                      type="email"
                      placeholder="usuario@empresa.com"
                      class="w-full h-11 px-4 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground bg-white"
                    />
                  </div>

                  <div>
                    <label
                      for="invite-password"
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      Contraseña
                    </label>
                    <div class="relative">
                      <input
                        id="invite-password"
                        v-model="inviteDraftPassword"
                        :type="showInvitePassword ? 'text' : 'password'"
                        placeholder="Mínimo 6 caracteres"
                        class="w-full h-11 px-4 pr-11 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground bg-white"
                      />
                      <button
                        type="button"
                        class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground cursor-pointer"
                        @click="showInvitePassword = !showInvitePassword"
                      >
                        <component
                          :is="showInvitePassword ? EyeOff : Eye"
                          class="w-4 h-4"
                        />
                      </button>
                    </div>
                  </div>

                  <div>
                    <label
                      for="invite-role"
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      Rol
                    </label>
                    <select
                      id="invite-role"
                      v-model="inviteRole"
                      class="w-full h-11 px-4 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 bg-white cursor-pointer"
                    >
                      <option value="employee">Empleado</option>
                      <option value="rider">Repartidor</option>
                    </select>
                  </div>

                  <div
                    v-if="inviteError"
                    class="rounded-lg bg-red-50 border border-red-200 px-4 py-3"
                  >
                    <p class="text-sm text-red-600">
                      {{ inviteError }}
                    </p>
                  </div>
                </div>

                <div class="mt-6 flex justify-end gap-3">
                  <button
                    v-if="inviteSuccess"
                    type="button"
                    class="h-10 px-5 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer"
                    @click="closeInviteDialog"
                  >
                    Cerrar
                  </button>
                  <template v-else>
                    <button
                      type="button"
                      class="h-10 px-4 border text-muted-foreground text-sm font-semibold rounded-lg hover:bg-muted transition-all duration-200 cursor-pointer"
                      @click="closeInviteDialog"
                    >
                      Cancelar
                    </button>
                    <button
                      type="button"
                      :disabled="inviting"
                      class="flex items-center gap-2 h-10 px-5 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
                      @click="sendInvite"
                    >
                      <Loader2 v-if="inviting" class="w-4 h-4 animate-spin" />
                      <span>{{
                        inviting ? "Creando..." : "Crear cuenta"
                      }}</span>
                    </button>
                  </template>
                </div>
              </div>
            </div>
          </Transition>
        </Teleport>
      </div>

      <!-- Tab: Integraciones -->
      <div v-if="activeTab === 'integraciones'">
        <div class="rounded-xl border bg-card shadow-sm max-w-2xl">
          <div class="px-6 py-5 border-b flex items-center justify-between">
            <div class="flex items-center gap-3">
              <div
                class="w-9 h-9 rounded-lg bg-primary/10 flex items-center justify-center"
              >
                <Zap class="w-5 h-5 text-primary" />
              </div>
              <div>
                <h2 class="text-lg font-bold font-heading text-foreground">
                  Integracion Fiscal (SENIAT)
                </h2>
                <p class="text-xs text-muted-foreground">
                  Conecta con el proveedor SENIAT para emitir comprobantes
                  fiscales
                </p>
              </div>
            </div>
            <!-- Status badge -->
            <span
              v-if="!loadingFiscal"
              :class="[
                'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold',
                fiscalIntegration?.active
                  ? 'bg-green-100 text-green-700'
                  : 'bg-muted text-muted-foreground',
              ]"
            >
              {{
                fiscalIntegration?.active ? "Conexion activa" : "Sin configurar"
              }}
            </span>
          </div>

          <div class="px-6 py-5">
            <div v-if="loadingFiscal">
              <div class="h-11 animate-pulse bg-muted rounded-lg" />
            </div>

            <template v-else>
              <!-- Current key (masked) -->
              <div
                v-if="
                  fiscalIntegration?.active && fiscalIntegration?.api_key_masked
                "
                class="mb-4 rounded-lg bg-green-50 border border-green-200 px-4 py-3 flex items-center justify-between"
              >
                <div>
                  <p class="text-xs font-semibold text-green-700 mb-0.5">
                    Clave actual
                  </p>
                  <p class="text-sm font-mono text-green-800">
                    {{ fiscalIntegration.api_key_masked }}
                  </p>
                </div>
              </div>

              <!-- Owner can edit -->
              <template v-if="store.isOwner">
                <div class="space-y-3">
                  <div>
                    <label
                      for="api-key"
                      class="block text-sm font-semibold text-foreground mb-1.5"
                    >
                      {{
                        fiscalIntegration?.active
                          ? "Actualizar clave de API"
                          : "Clave de API SENIAT"
                      }}
                    </label>
                    <div class="relative">
                      <input
                        id="api-key"
                        v-model="apiKey"
                        :type="showApiKey ? 'text' : 'password'"
                        placeholder="Ingresa la clave del proveedor"
                        class="w-full h-11 px-4 pr-11 border rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground bg-white"
                      />
                      <button
                        type="button"
                        class="absolute inset-y-0 right-0 flex items-center px-3 text-muted-foreground hover:text-[hsl(var(--foreground))] transition-colors duration-200 cursor-pointer"
                        @click="showApiKey = !showApiKey"
                      >
                        <component
                          :is="showApiKey ? EyeOff : Eye"
                          class="w-4 h-4"
                        />
                      </button>
                    </div>
                  </div>

                  <div
                    v-if="fiscalSaveError"
                    class="rounded-lg bg-red-50 border border-red-200 px-4 py-3"
                  >
                    <p class="text-sm text-red-600">
                      {{ fiscalSaveError }}
                    </p>
                  </div>

                  <div
                    v-if="fiscalSaveSuccess"
                    class="rounded-lg bg-green-50 border border-green-200 px-4 py-3"
                  >
                    <p class="text-sm text-green-700 font-medium">
                      Clave guardada correctamente.
                    </p>
                  </div>

                  <button
                    type="button"
                    :disabled="savingFiscal"
                    class="flex items-center gap-2 h-10 px-5 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
                    @click="saveFiscalKey"
                  >
                    <Loader2 v-if="savingFiscal" class="w-4 h-4 animate-spin" />
                    <Save v-else class="w-4 h-4" />
                    Guardar
                  </button>
                </div>
              </template>

              <!-- Read-only view for non-owners -->
              <template v-else>
                <div class="rounded-lg bg-muted px-4 py-4">
                  <p class="text-sm text-muted-foreground">
                    Solo el propietario puede configurar la integracion fiscal.
                  </p>
                  <p
                    v-if="fiscalIntegration?.api_key_masked"
                    class="text-sm font-mono text-foreground mt-2"
                  >
                    Clave: {{ fiscalIntegration.api_key_masked }}
                  </p>
                </div>
              </template>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
