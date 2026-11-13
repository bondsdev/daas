<script setup lang="ts">
import {
  LayoutDashboard,
  Package,
  Warehouse,
  ShoppingCart,
  FileText,
  Truck,
  BarChart2,
  Wallet,
  Settings,
  LogOut,
  ChevronRight,
  Bell,
  Search,
  Command,
  PanelLeft,
  CreditCard,
  Palette,
  Store,
} from "lucide-vue-next";
import { useAuthStore } from "~/stores/auth";

const store = useAuthStore();
const route = useRoute();
// isMobile drives the exclusive mobile experience (native app OR ?m=1 preview).
// Web at any width is unaffected unless the preview flag is set.
const { isMobile } = useMobileMode();
// Pushed detail screens (MobileScreen with a back button) hide the tab bar.
const navHidden = useState("mobile:navHidden", () => false);

// sidebarOpen starts false — CSS breakpoints handle position (static vs fixed),
// so there's no position-change flash. onMounted opens it on desktop.
const sidebarOpen = ref(false);
const openGroups = ref<Set<string>>(new Set());

interface NavSubItem {
  title: string;
  url: string;
}

interface NavItem {
  title: string;
  url: string;
  icon: Component;
  subItems?: NavSubItem[];
  featureGate?: string;
}

interface NavGroup {
  id: number;
  label: string;
  items: NavItem[];
}

const sidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "Principal",
    items: [{ title: "Dashboard", url: "/dashboard", icon: LayoutDashboard }],
  },
  {
    id: 2,
    label: "Catálogo",
    items: [
      {
        title: "Productos",
        url: "/products",
        icon: Package,
        subItems: [
          { title: "Lista de productos", url: "/products" },
          { title: "Categorías", url: "/products/categories" },
        ],
      },
      {
        title: "Inventario",
        url: "/inventory",
        icon: Warehouse,
        subItems: [
          { title: "Stock", url: "/inventory" },
          { title: "Movimientos", url: "/inventory/movements" },
        ],
      },
    ],
  },
  {
    id: 3,
    label: "Ventas",
    items: [
      {
        title: "Caja",
        url: "/cash-sessions",
        icon: Wallet,
      },
      {
        title: "Órdenes de venta",
        url: "/sales-orders",
        icon: ShoppingCart,
      },
      {
        title: "Pedidos online",
        url: "/shop-orders",
        icon: ShoppingCart,
        featureGate: "storefront",
      },
      {
        title: "Facturas",
        url: "/invoices",
        icon: FileText,
        subItems: [
          { title: "Todas las facturas", url: "/invoices" },
          { title: "Fiscales", url: "/invoices/fiscal" },
        ],
      },
    ],
  },
  {
    id: 4,
    label: "Operaciones",
    items: [
      {
        title: "Proveedores",
        url: "/suppliers",
        icon: Truck,
        subItems: [
          { title: "Lista de proveedores", url: "/suppliers" },
          { title: "Órdenes de compra", url: "/suppliers/purchase-orders" },
        ],
      },
    ],
  },
  {
    id: 5,
    label: "Análisis",
    items: [{ title: "Reportes", url: "/reports", icon: BarChart2 }],
  },
  {
    id: 6,
    label: "Configuración",
    items: [
      {
        title: "Métodos de pago",
        url: "/settings/payment-methods",
        icon: CreditCard,
      },
      {
        title: "Branding",
        url: "/settings/branding",
        icon: Palette,
      },
    ],
  },
];

function isActive(url: string) {
  return route.path === url || route.path.startsWith(url + "/");
}

function toggleGroup(url: string) {
  if (openGroups.value.has(url)) {
    openGroups.value.delete(url);
  } else {
    openGroups.value.add(url);
  }
}

function isGroupOpen(item: NavItem) {
  if (openGroups.value.has(item.url)) return true;
  return item.subItems?.some((s) => isActive(s.url)) ?? false;
}

async function handleLogout() {
  await store.logout();
  await navigateTo("/auth/login");
}

const userInitials = computed(() => {
  const email = store.user?.email ?? "";
  return email.slice(0, 2).toUpperCase();
});

const tenantLabel = computed(
  () => store.tenant?.name || store.user?.email || "",
);
const roleLabel = computed(() =>
  store.user?.role === "owner" ? "Propietario" : "Empleado",
);

const storefrontEnabled = computed(
  () => store.tenant?.features?.storefront === true,
);

// Close the mobile drawer on navigation
watch(
  () => route.path,
  () => {
    if (window.innerWidth < 1024) sidebarOpen.value = false;
  },
);

onMounted(() => {
  // Open sidebar immediately on desktop — client-only layout, no SSR mismatch risk.
  // In mobile mode the sidebar is never shown, so we skip this entirely.
  if (!isMobile.value && window.innerWidth >= 1024) sidebarOpen.value = true;

  const handleKeyboard = (e: KeyboardEvent) => {
    if (isMobile.value) return; // no keyboard shortcuts on mobile
    if ((e.metaKey || e.ctrlKey) && e.key === "b") {
      e.preventDefault();
      sidebarOpen.value = !sidebarOpen.value;
    }
    if (e.key === "Escape" && window.innerWidth < 1024)
      sidebarOpen.value = false;
  };
  window.addEventListener("keydown", handleKeyboard);

  onUnmounted(() => {
    window.removeEventListener("keydown", handleKeyboard);
  });
});
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-background">
    <!-- ── Mobile backdrop (hidden in mobile mode + lg+) ─── -->
    <Transition name="fade">
      <div
        v-if="sidebarOpen && !isMobile"
        class="fixed inset-0 z-30 bg-black/50 lg:hidden"
        aria-hidden="true"
        @click="sidebarOpen = false"
      />
    </Transition>

    <!-- ── Sidebar (NOT rendered in mobile mode) ─────────── -->
    <!--
      Web layout strategy (CSS handles small-vs-large):
        small  : fixed + full-width drawer; translate controls show/hide
        large  : static in flex flow; width controls collapse/expand
      Mobile  : v-if="!isMobile" → sidebar removed; bottom nav takes over
    -->
    <aside
      v-if="!isMobile"
      class="flex flex-col shrink-0 border-r border-border bg-sidebar overflow-hidden transition-[width,transform] duration-200 fixed inset-y-0 left-0 z-40 w-64 shadow-xl lg:static lg:inset-auto lg:z-auto lg:shadow-none"
      :class="{
        'translate-x-0': sidebarOpen,
        '-translate-x-full lg:translate-x-0': !sidebarOpen,
        'lg:w-64': sidebarOpen,
        'lg:w-14': !sidebarOpen,
      }"
    >
      <!-- Logo -->
      <div class="flex h-12 shrink-0 items-center border-b border-border px-3">
        <div class="flex items-center gap-2 min-w-0">
          <Command class="size-4 shrink-0 text-primary" />
          <span
            v-if="sidebarOpen"
            class="font-bold text-sm truncate text-primary tracking-tight"
            >DaaS</span
          >
        </div>
      </div>

      <!-- Nav -->
      <nav class="flex-1 overflow-y-auto py-2 px-2">
        <div v-for="group in sidebarItems" :key="group.id" class="mb-4">
          <p
            v-if="sidebarOpen"
            class="mb-1 px-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground"
          >
            {{ group.label }}
          </p>
          <div v-else class="mb-1 h-4" />

          <ul class="space-y-0.5">
            <li
              v-for="item in group.items"
              v-show="
                !item.featureGate ||
                (item.featureGate === 'storefront' && storefrontEnabled)
              "
              :key="item.url"
            >
              <!-- Item with subitems -->
              <template v-if="item.subItems">
                <button
                  type="button"
                  class="w-full flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors text-foreground hover:bg-accent"
                  :class="isActive(item.url) ? 'bg-accent font-medium' : ''"
                  :title="!sidebarOpen ? item.title : undefined"
                  @click="toggleGroup(item.url)"
                >
                  <component :is="item.icon" class="size-4 shrink-0" />
                  <span v-if="sidebarOpen" class="flex-1 text-left truncate">{{
                    item.title
                  }}</span>
                  <ChevronRight
                    v-if="sidebarOpen"
                    class="size-3.5 shrink-0 text-muted-foreground transition-transform duration-150"
                    :class="isGroupOpen(item) ? 'rotate-90' : ''"
                  />
                </button>
                <ul
                  v-if="sidebarOpen && isGroupOpen(item)"
                  class="mt-0.5 ml-4 space-y-0.5 border-l border-border pl-2"
                >
                  <li v-for="sub in item.subItems" :key="sub.url">
                    <NuxtLink
                      :to="sub.url"
                      class="block rounded-md px-2 py-1.5 text-sm transition-colors"
                      :class="
                        isActive(sub.url)
                          ? 'bg-accent text-accent-foreground font-medium'
                          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                      "
                    >
                      {{ sub.title }}
                    </NuxtLink>
                  </li>
                </ul>
              </template>

              <!-- Simple item -->
              <NuxtLink
                v-else
                :to="item.url"
                class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors"
                :class="
                  isActive(item.url)
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-foreground hover:bg-accent'
                "
                :title="!sidebarOpen ? item.title : undefined"
              >
                <component :is="item.icon" class="size-4 shrink-0" />
                <span v-if="sidebarOpen" class="truncate">{{
                  item.title
                }}</span>
              </NuxtLink>
            </li>
          </ul>
        </div>

        <!-- Ver mi tienda — only shown when storefront feature is enabled -->
        <div v-if="storefrontEnabled && store.tenant?.slug" class="mb-2">
          <a
            :href="`/t/${store.tenant.slug}`"
            target="_blank"
            rel="noopener noreferrer"
            class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors text-foreground hover:bg-accent"
          >
            <Store class="size-4 shrink-0" />
            <span v-if="sidebarOpen" class="truncate">Ver mi tienda</span>
          </a>
        </div>
      </nav>

      <!-- Footer -->
      <div class="shrink-0 border-t border-border p-2 space-y-0.5">
        <NuxtLink
          to="/settings"
          class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors"
          :class="
            isActive('/settings')
              ? 'bg-accent text-accent-foreground font-medium'
              : 'text-foreground hover:bg-accent'
          "
          :title="!sidebarOpen ? 'Configuración' : undefined"
        >
          <Settings class="size-4 shrink-0" />
          <span v-if="sidebarOpen" class="truncate">Configuración</span>
        </NuxtLink>

        <div class="flex items-center gap-2 rounded-md px-2 py-1.5">
          <div
            class="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground text-xs font-semibold"
          >
            {{ userInitials }}
          </div>
          <div
            v-if="sidebarOpen"
            class="grid flex-1 min-w-0 text-sm leading-tight"
          >
            <span class="truncate font-medium text-foreground">{{
              tenantLabel
            }}</span>
            <span class="truncate text-xs text-muted-foreground">{{
              roleLabel
            }}</span>
          </div>
          <button
            v-if="sidebarOpen"
            type="button"
            title="Cerrar sesión"
            class="ml-auto rounded-md p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors"
            @click="handleLogout"
          >
            <LogOut class="size-4" />
          </button>
        </div>
      </div>
    </aside>

    <!-- ── Main area ───────────────────────────────────── -->
    <div class="flex flex-1 flex-col min-w-0 overflow-hidden">
      <!-- ── Web header (browser / desktop) — hidden in mobile mode ── -->
      <header
        v-if="!isMobile"
        class="flex h-12 shrink-0 items-center border-b border-border bg-sidebar"
      >
        <div class="flex w-full items-center justify-between px-4">
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="inline-flex size-7 items-center justify-center rounded-md text-foreground hover:bg-accent transition-colors"
              @click="sidebarOpen = !sidebarOpen"
            >
              <PanelLeft class="size-4" />
            </button>
            <div class="h-4 w-px bg-border" />
            <button
              type="button"
              class="inline-flex items-center gap-2 rounded-md border border-input bg-background px-2.5 h-7 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
            >
              <Search class="size-3.5" />
              <span>Buscar…</span>
              <kbd
                class="ml-2 hidden sm:inline-flex items-center gap-0.5 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
              >
                <span>⌘</span>K
              </kbd>
            </button>
          </div>
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="inline-flex size-7 items-center justify-center rounded-md text-foreground hover:bg-accent transition-colors"
            >
              <Bell class="size-4" />
            </button>
          </div>
        </div>
      </header>

      <!-- Page content
           Mobile: no chrome here — each screen mounts its own MobileScreen
                   (contextual top bar + scroll region + safe areas).
           Web: normal scroll container.
      -->
      <main
        class="flex-1 min-h-0"
        :class="isMobile ? 'overflow-hidden' : 'overflow-y-auto'"
      >
        <slot />
      </main>
    </div>

    <!-- ── Bottom navigation (mobile tabs; hidden on pushed details) ── -->
    <MobileBottomNav v-if="isMobile && !navHidden" />
  </div>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
