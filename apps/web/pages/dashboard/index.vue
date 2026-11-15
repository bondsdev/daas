<script setup lang="ts">
import {
  ShoppingBag,
  Package,
  ClipboardList,
  ArrowRight,
  Loader2,
  RefreshCw,
} from "lucide-vue-next";
import { useAuthStore } from "~/stores/auth";
import { useApiFetch } from "~/composables/useAuth";
import { Card, CardHeader, CardTitle, CardContent } from "~/components/ui/card";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

const store = useAuthStore();
const { isMobile } = useMobileMode();

// ─── Types ────────────────────────────────────────────────────────────────────

interface PeriodStats {
  b2b_revenue: number;
  b2c_revenue: number;
  invoice_count: number;
}

interface PendingStats {
  shop_orders: number;
  sales_orders: number;
}

interface InventoryStats {
  low_stock: number;
  out_of_stock: number;
}

interface ShopOrderSummary {
  id: string;
  customer_name: string;
  customer_email: string;
  total: number;
  status: string;
  created_at: string;
}

interface DashboardResponse {
  today: PeriodStats;
  month: PeriodStats;
  pending: PendingStats;
  inventory: InventoryStats;
  recent_shop_orders: ShopOrderSummary[];
}

// ─── State ────────────────────────────────────────────────────────────────────

const loading = ref(false);
const loadError = ref("");
const data = ref<DashboardResponse | null>(null);

// ─── Helpers ──────────────────────────────────────────────────────────────────

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

function fmtDate(d: string) {
  return new Date(d).toLocaleDateString("es-VE", {
    day: "2-digit",
    month: "short",
  });
}

// ─── Status badge config ──────────────────────────────────────────────────────

const SHOP_STATUS: Record<string, { label: string; class: string }> = {
  pending: {
    label: "Pendiente",
    class: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  },
  paid: {
    label: "Pagada",
    class: "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
  },
  fulfilled: {
    label: "Preparada",
    class:
      "bg-purple-50 text-purple-700 dark:bg-purple-950 dark:text-purple-300",
  },
  delivered: {
    label: "Entregada",
    class:
      "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
  },
  cancelled: {
    label: "Cancelada",
    class: "bg-rose-50 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
  },
};

// ─── Load ─────────────────────────────────────────────────────────────────────

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    data.value = await useApiFetch<DashboardResponse>("/api/v1/dashboard");
  } catch {
    loadError.value = "No se pudieron cargar los datos del dashboard";
  } finally {
    loading.value = false;
  }
}

// ─── Computed figures ─────────────────────────────────────────────────────────

const todayRevenue = computed(() =>
  fmtCurrency(
    (data.value?.today.b2b_revenue ?? 0) + (data.value?.today.b2c_revenue ?? 0),
  ),
);
const todayB2BRevenue = computed(() =>
  fmtCurrency(data.value?.today.b2b_revenue ?? 0),
);
const todayB2CRevenue = computed(() =>
  fmtCurrency(data.value?.today.b2c_revenue ?? 0),
);

const monthRevenue = computed(() =>
  fmtCurrency(
    (data.value?.month.b2b_revenue ?? 0) + (data.value?.month.b2c_revenue ?? 0),
  ),
);

const monthB2BRevenue = computed(() =>
  fmtCurrency(data.value?.month.b2b_revenue ?? 0),
);
const monthB2CRevenue = computed(() =>
  fmtCurrency(data.value?.month.b2c_revenue ?? 0),
);

onMounted(load);
</script>

<template>
  <!-- ════════════════════════════════════════════════════════════════
       MOBILE — exclusive native screen (presentational component)
  ═════════════════════════════════════════════════════════════════ -->
  <MobileScreensDashboard
    v-if="isMobile"
    :data="data"
    :loading="loading"
    :load-error="loadError"
    :tenant-name="store.tenant?.name || store.user?.email || ''"
    @reload="load"
  />

  <!-- ════════════════════════════════════════════════════════════════
       WEB — unchanged desktop dashboard
  ═════════════════════════════════════════════════════════════════ -->
  <div v-else class="p-4 sm:p-6 space-y-4 max-w-7xl mx-auto">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-xl font-semibold tracking-tight">Dashboard</h1>
        <p class="text-sm text-muted-foreground">
          {{ store.tenant?.name || store.user?.email }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <NuxtLink
          to="/inventory"
          class="inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-xs font-medium hover:bg-accent transition-colors"
        >
          <Package class="size-3.5" /> Inventario
        </NuxtLink>
        <NuxtLink
          to="/reports"
          class="inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-xs font-medium hover:bg-accent transition-colors"
        >
          <ClipboardList class="size-3.5" /> Reportes
        </NuxtLink>
        <NuxtLink
          to="/sales-orders"
          class="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground hover:opacity-90 transition-opacity"
        >
          <ShoppingBag class="size-3.5" /> Nueva venta
        </NuxtLink>
        <button
          type="button"
          :disabled="loading"
          class="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors disabled:opacity-40"
          @click="load"
        >
          <RefreshCw :class="['w-4 h-4', loading ? 'animate-spin' : '']" />
        </button>
      </div>
    </div>

    <!-- Error -->
    <div
      v-if="loadError"
      class="rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive"
    >
      {{ loadError }}
    </div>

    <!-- Actionable indicators strip — text-first, no decorative tiles -->
    <div
      class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 rounded-lg border divide-y sm:divide-y-0 sm:divide-x divide-border overflow-hidden"
    >
      <NuxtLink
        to="/shop-orders?status=pending"
        class="flex items-center gap-3 px-4 py-3.5 hover:bg-accent transition-colors"
      >
        <span
          class="size-2 rounded-full bg-amber-500 shrink-0"
          aria-hidden="true"
        />
        <span class="text-lg font-semibold tabular-nums">{{
          data?.pending.shop_orders ?? 0
        }}</span>
        <span class="text-sm text-muted-foreground">pedidos sin pagar</span>
      </NuxtLink>
      <NuxtLink
        to="/sales-orders?status=confirmed"
        class="flex items-center gap-3 px-4 py-3.5 hover:bg-accent transition-colors"
      >
        <span
          class="size-2 rounded-full bg-blue-500 shrink-0"
          aria-hidden="true"
        />
        <span class="text-lg font-semibold tabular-nums">{{
          data?.pending.sales_orders ?? 0
        }}</span>
        <span class="text-sm text-muted-foreground"
          >órdenes B2B para facturar</span
        >
      </NuxtLink>
      <NuxtLink
        to="/inventory"
        class="flex items-center gap-3 px-4 py-3.5 hover:bg-accent transition-colors"
      >
        <span
          class="size-2 rounded-full bg-rose-500 shrink-0"
          aria-hidden="true"
        />
        <span class="text-lg font-semibold tabular-nums">{{
          data?.inventory.out_of_stock ?? 0
        }}</span>
        <span class="text-sm text-muted-foreground">productos sin stock</span>
      </NuxtLink>
      <NuxtLink
        to="/inventory"
        class="flex items-center gap-3 px-4 py-3.5 hover:bg-accent transition-colors"
      >
        <span
          class="size-2 rounded-full bg-amber-500 shrink-0"
          aria-hidden="true"
        />
        <span class="text-lg font-semibold tabular-nums">{{
          data?.inventory.low_stock ?? 0
        }}</span>
        <span class="text-sm text-muted-foreground">con stock bajo</span>
      </NuxtLink>
    </div>

    <!-- Activity table + Hoy/Mes side panel -->
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_280px]">
      <Card>
        <CardHeader class="flex flex-row items-center justify-between py-4">
          <CardTitle class="text-sm"
            >Pedidos recientes — tienda online</CardTitle
          >
          <NuxtLink
            to="/shop-orders"
            class="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            Ver todos <ArrowRight class="size-3" />
          </NuxtLink>
        </CardHeader>
        <CardContent class="px-0">
          <div v-if="loading && !data" class="px-4 py-8 flex justify-center">
            <Loader2 class="w-5 h-5 animate-spin text-muted-foreground" />
          </div>
          <div
            v-else-if="!data?.recent_shop_orders.length"
            class="px-4 py-8 text-center text-sm text-muted-foreground"
          >
            Sin pedidos aún
          </div>
          <table v-else class="w-full text-sm">
            <thead>
              <tr class="text-xs text-muted-foreground uppercase tracking-wide">
                <th class="text-left font-medium px-4 py-2">Cliente</th>
                <th class="text-left font-medium px-4 py-2">Fecha</th>
                <th class="text-right font-medium px-4 py-2">Total</th>
                <th class="text-left font-medium px-4 py-2">Estado</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="order in data.recent_shop_orders"
                :key="order.id"
                class="border-t hover:bg-accent/40 transition-colors"
              >
                <td class="px-4 py-2.5">
                  <NuxtLink
                    :to="`/shop-orders/${order.id}`"
                    class="font-medium hover:underline"
                  >
                    {{
                      order.customer_name ||
                      order.customer_email ||
                      "Cliente anónimo"
                    }}
                  </NuxtLink>
                </td>
                <td class="px-4 py-2.5 text-muted-foreground text-xs">
                  {{ fmtDate(order.created_at) }}
                </td>
                <td class="px-4 py-2.5 text-right font-mono tabular-nums">
                  {{ fmtCurrency(order.total) }}
                </td>
                <td class="px-4 py-2.5">
                  <span
                    :class="[
                      'inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium',
                      SHOP_STATUS[order.status]?.class,
                    ]"
                  >
                    {{ SHOP_STATUS[order.status]?.label ?? order.status }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </CardContent>
      </Card>

      <aside class="flex flex-col gap-4">
        <Card>
          <CardContent class="pt-5">
            <p
              class="text-xs uppercase tracking-wide text-muted-foreground font-medium mb-2"
            >
              Hoy
            </p>
            <p class="text-2xl font-semibold tabular-nums">
              {{ todayRevenue }}
            </p>
            <p class="text-xs text-muted-foreground mt-1">
              {{ data?.today.invoice_count ?? 0 }} facturas emitidas
            </p>
            <div class="mt-3 pt-3 border-t text-xs space-y-1.5">
              <div class="flex justify-between">
                <span class="text-muted-foreground">B2B</span>
                <span class="font-medium tabular-nums">{{
                  todayB2BRevenue
                }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-muted-foreground">Tienda online</span>
                <span class="font-medium tabular-nums">{{
                  todayB2CRevenue
                }}</span>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-5">
            <p
              class="text-xs uppercase tracking-wide text-muted-foreground font-medium mb-2"
            >
              Mes
            </p>
            <p class="text-2xl font-semibold tabular-nums">
              {{ monthRevenue }}
            </p>
            <p class="text-xs text-muted-foreground mt-1">
              {{ data?.month.invoice_count ?? 0 }} facturas emitidas
            </p>
            <div class="mt-3 pt-3 border-t text-xs space-y-1.5">
              <div class="flex justify-between">
                <span class="text-muted-foreground">B2B</span>
                <span class="font-medium tabular-nums">{{
                  monthB2BRevenue
                }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-muted-foreground">Tienda online</span>
                <span class="font-medium tabular-nums">{{
                  monthB2CRevenue
                }}</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </aside>
    </div>
  </div>
</template>
