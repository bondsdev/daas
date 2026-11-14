<script setup lang="ts">
import {
  Wallet,
  Lock,
  Unlock,
  Loader2,
  AlertTriangle,
  CreditCard,
  Users,
  ShoppingCart,
  Package,
  ArrowRight,
} from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";
import CloseCashSessionModal from "~/components/cash-sessions/CloseCashSessionModal.vue";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

// ─── Types ────────────────────────────────────────────────────────────────────

interface CashSession {
  id: string;
  tenant_id: string;
  status: "open" | "closed";
  exchange_rate: number;
  exchange_rate_source: "auto" | "manual";
  opening_notes: string;
  closing_notes: string;
  opened_by: string;
  opened_at: string;
  closed_by: string | null;
  closed_at: string | null;
}

interface PaymentMethodTotal {
  method: string;
  total: number;
  count: number;
}

interface ProductRevenue {
  product_id: string;
  product_name: string;
  total_sold: number;
  revenue: number;
}

interface InventorySnapshot {
  total_products: number;
  low_stock_count: number;
  out_of_stock_count: number;
}

interface CashSessionSummary {
  total_bs: number;
  total_usd: number;
  sale_count: number;
  customer_count: number;
  by_payment_method: PaymentMethodTotal[];
  products_sold: ProductRevenue[];
  inventory_remaining: InventorySnapshot | null;
}

// ─── State ────────────────────────────────────────────────────────────────────

const loading = ref(true);
const loadError = ref("");
const openSession = ref<CashSession | null>(null);

const manualRate = ref<number | null>(null);
const openingNotes = ref("");
const opening = ref(false);
const openError = ref("");

const closeModalOpen = ref(false);

const summary = ref<CashSessionSummary | null>(null);
const summaryLoading = ref(false);
const summaryError = ref("");

// ─── Data loading ─────────────────────────────────────────────────────────────

async function fetchOpenSession() {
  loading.value = true;
  loadError.value = "";
  try {
    openSession.value = await useApiFetch<CashSession>(
      "/api/v1/cash-sessions/open",
    );
  } catch (err: unknown) {
    const status =
      (err as { response?: { status?: number } })?.response?.status ??
      (err as { status?: number })?.status;
    if (status === 404) {
      openSession.value = null;
    } else {
      loadError.value = "Error al consultar el estado de la caja";
    }
  } finally {
    loading.value = false;
  }
}

async function openCashSession() {
  openError.value = "";
  opening.value = true;
  try {
    openSession.value = await useApiFetch<CashSession>(
      "/api/v1/cash-sessions/open",
      {
        method: "POST",
        body: {
          manual_rate: manualRate.value,
          notes: openingNotes.value.trim(),
        },
      },
    );
    summary.value = null;
    manualRate.value = null;
    openingNotes.value = "";
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string; message?: string } };
    openError.value =
      e?.data?.detail ?? e?.data?.message ?? "Error al abrir la caja";
  } finally {
    opening.value = false;
  }
}

async function onClosed(sessionId: string) {
  openSession.value = null;
  summaryLoading.value = true;
  summaryError.value = "";
  try {
    summary.value = await useApiFetch<CashSessionSummary>(
      `/api/v1/cash-sessions/${sessionId}/summary`,
    );
  } catch {
    summaryError.value = "Error al cargar el resumen de cierre";
  } finally {
    summaryLoading.value = false;
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatDate(iso: string) {
  return new Date(iso).toLocaleString("es-VE", {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const paymentMethodLabels: Record<string, string> = {
  cash: "Efectivo",
  card: "Tarjeta",
  mobile_payment: "Pago móvil",
};

function paymentMethodLabel(method: string) {
  return paymentMethodLabels[method] ?? method;
}

onMounted(fetchOpenSession);
</script>

<template>
  <div class="p-4 sm:p-6 max-w-4xl">
    <!-- Page header -->
    <div class="mb-8 flex items-center gap-3">
      <div
        class="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center"
      >
        <Wallet class="w-5 h-5 text-primary" />
      </div>
      <div>
        <h1 class="text-2xl font-bold font-heading text-foreground">Caja</h1>
        <p class="text-muted-foreground text-sm">
          Apertura, cierre y resumen de la sesión de caja
        </p>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="rounded-xl border bg-card border-border p-6">
      <div class="h-24 animate-pulse bg-gray-100 rounded-lg" />
    </div>

    <!-- Error -->
    <div
      v-else-if="loadError"
      class="rounded-xl border bg-card border-border p-6 text-center"
    >
      <AlertTriangle class="w-10 h-10 text-red-400 mx-auto mb-3" />
      <p class="text-red-600 font-medium">{{ loadError }}</p>
      <button
        type="button"
        class="mt-4 h-9 px-4 border border-input text-sm font-semibold text-muted-foreground rounded-lg hover:bg-gray-50 cursor-pointer"
        @click="fetchOpenSession"
      >
        Reintentar
      </button>
    </div>

    <!-- No open session: open form -->
    <div
      v-else-if="!openSession"
      class="rounded-xl border bg-card border-border shadow-sm p-6"
    >
      <div class="flex items-center gap-3 mb-5">
        <div
          class="w-9 h-9 rounded-lg bg-orange-50 flex items-center justify-center"
        >
          <Lock class="w-4 h-4 text-orange-600" />
        </div>
        <div>
          <h2 class="text-base font-bold font-heading text-foreground">
            La caja está cerrada
          </h2>
          <p class="text-sm text-muted-foreground">
            Abre una sesión para empezar a vender
          </p>
        </div>
      </div>

      <div
        v-if="openError"
        class="mb-4 rounded-lg bg-red-50 border border-red-200 px-4 py-3 flex items-start gap-2"
      >
        <AlertTriangle class="w-4 h-4 text-red-500 flex-shrink-0 mt-0.5" />
        <p class="text-sm text-red-600">{{ openError }}</p>
      </div>

      <div class="space-y-4">
        <div>
          <label
            for="manual-rate"
            class="block text-sm font-semibold text-foreground mb-1.5"
          >
            Tasa manual BS/USD
            <span class="text-muted-foreground font-normal ml-1"
              >(opcional — si se deja vacío se busca automáticamente)</span
            >
          </label>
          <input
            id="manual-rate"
            v-model.number="manualRate"
            type="number"
            step="0.01"
            placeholder="Ej: 45.50"
            class="w-full h-11 px-4 border border-input rounded-lg text-base transition-all duration-200 focus:outline-none focus:ring-2 focus:border-primary focus:ring-primary/20 placeholder:text-muted-foreground"
          />
        </div>

        <div>
          <label
            for="opening-notes"
            class="block text-sm font-semibold text-foreground mb-1.5"
          >
            Notas de apertura
            <span class="text-muted-foreground font-normal ml-1"
              >(opcional)</span
            >
          </label>
          <textarea
            id="opening-notes"
            v-model="openingNotes"
            rows="2"
            placeholder="Ej: Apertura de turno mañana..."
            class="w-full px-4 py-3 border border-input rounded-lg text-base transition-all duration-200 resize-none focus:outline-none focus:ring-2 focus:border-primary focus:ring-primary/20 placeholder:text-muted-foreground"
          />
        </div>

        <button
          type="button"
          :disabled="opening"
          class="flex items-center gap-2 h-11 px-6 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
          @click="openCashSession"
        >
          <Loader2 v-if="opening" class="w-4 h-4 animate-spin" />
          <Unlock v-else class="w-4 h-4" />
          <span>{{ opening ? "Abriendo..." : "Abrir caja" }}</span>
        </button>
      </div>
    </div>

    <!-- Open session -->
    <div v-else class="space-y-6">
      <div class="rounded-xl border bg-card border-border shadow-sm p-6">
        <div class="flex items-center justify-between gap-4 flex-wrap">
          <div class="flex items-center gap-3">
            <div
              class="w-9 h-9 rounded-lg bg-green-50 flex items-center justify-center"
            >
              <Unlock class="w-4 h-4 text-green-600" />
            </div>
            <div>
              <h2 class="text-base font-bold font-heading text-foreground">
                Caja abierta
              </h2>
              <p class="text-sm text-muted-foreground">
                Desde {{ formatDate(openSession.opened_at) }}
              </p>
            </div>
          </div>
          <button
            type="button"
            class="flex items-center gap-2 h-10 px-5 border border-input text-sm font-semibold text-foreground rounded-lg hover:bg-gray-50 transition-all duration-200 cursor-pointer"
            @click="closeModalOpen = true"
          >
            <Lock class="w-4 h-4" />
            Cerrar caja
          </button>
        </div>

        <div class="grid grid-cols-2 gap-4 mt-6">
          <div class="rounded-lg bg-gray-50 px-4 py-3">
            <p
              class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
            >
              Tasa BS/USD
            </p>
            <p class="text-lg font-bold font-heading text-foreground">
              {{ openSession.exchange_rate.toFixed(2) }}
              <span class="text-xs font-normal text-muted-foreground ml-1">
                ({{
                  openSession.exchange_rate_source === "auto"
                    ? "automática"
                    : "manual"
                }})
              </span>
            </p>
          </div>
          <div class="rounded-lg bg-gray-50 px-4 py-3">
            <p
              class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
            >
              Notas de apertura
            </p>
            <p class="text-sm text-foreground">
              {{ openSession.opening_notes || "—" }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Closing summary -->
    <div
      v-if="summaryLoading"
      class="mt-6 rounded-xl border bg-card border-border p-6"
    >
      <div class="h-32 animate-pulse bg-gray-100 rounded-lg" />
    </div>

    <div
      v-else-if="summaryError"
      class="mt-6 rounded-xl border bg-card border-border p-6 text-center"
    >
      <AlertTriangle class="w-8 h-8 text-red-400 mx-auto mb-2" />
      <p class="text-red-600 text-sm">{{ summaryError }}</p>
    </div>

    <div v-else-if="summary" class="mt-6 space-y-4">
      <h2 class="text-base font-bold font-heading text-foreground">
        Resumen del cierre
      </h2>

      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div
          class="rounded-xl border bg-card border-border shadow-sm px-5 py-4"
        >
          <p
            class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
          >
            Total BS
          </p>
          <p class="text-2xl font-bold font-heading text-foreground">
            {{ summary.total_bs.toFixed(2) }}
          </p>
        </div>
        <div
          class="rounded-xl border bg-card border-border shadow-sm px-5 py-4"
        >
          <p
            class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
          >
            Total USD
          </p>
          <p class="text-2xl font-bold font-heading text-foreground">
            ${{ summary.total_usd.toFixed(2) }}
          </p>
        </div>
        <div
          class="rounded-xl border bg-card border-border shadow-sm px-5 py-4"
        >
          <p
            class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
          >
            Ventas
          </p>
          <p
            class="text-2xl font-bold font-heading text-foreground flex items-center gap-1.5"
          >
            <ShoppingCart class="w-4 h-4 text-muted-foreground" />
            {{ summary.sale_count }}
          </p>
        </div>
        <div
          class="rounded-xl border bg-card border-border shadow-sm px-5 py-4"
        >
          <p
            class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1"
          >
            Clientes
          </p>
          <p
            class="text-2xl font-bold font-heading text-foreground flex items-center gap-1.5"
          >
            <Users class="w-4 h-4 text-muted-foreground" />
            {{ summary.customer_count }}
          </p>
        </div>
      </div>

      <!-- By payment method -->
      <div class="rounded-xl border bg-card border-border shadow-sm">
        <div class="px-6 py-4 border-b border-border flex items-center gap-2">
          <CreditCard class="w-4 h-4 text-muted-foreground" />
          <h3 class="text-sm font-bold font-heading text-foreground">
            Por método de pago
          </h3>
        </div>
        <div
          v-if="summary.by_payment_method.length === 0"
          class="px-6 py-6 text-sm text-muted-foreground text-center"
        >
          Sin ventas registradas
        </div>
        <div v-else class="divide-y divide-border">
          <div
            v-for="pm in summary.by_payment_method"
            :key="pm.method"
            class="px-6 py-3 flex items-center justify-between text-sm"
          >
            <span class="text-foreground font-medium">{{
              paymentMethodLabel(pm.method)
            }}</span>
            <span class="text-muted-foreground"
              >{{ pm.count }} venta{{ pm.count !== 1 ? "s" : "" }}</span
            >
            <span class="font-semibold text-foreground">{{
              pm.total.toFixed(2)
            }}</span>
          </div>
        </div>
      </div>

      <!-- Products sold -->
      <div class="rounded-xl border bg-card border-border shadow-sm">
        <div class="px-6 py-4 border-b border-border flex items-center gap-2">
          <Package class="w-4 h-4 text-muted-foreground" />
          <h3 class="text-sm font-bold font-heading text-foreground">
            Productos vendidos
          </h3>
        </div>
        <div
          v-if="summary.products_sold.length === 0"
          class="px-6 py-6 text-sm text-muted-foreground text-center"
        >
          Sin productos vendidos
        </div>
        <div v-else class="divide-y divide-border">
          <div
            v-for="p in summary.products_sold"
            :key="p.product_id"
            class="px-6 py-3 flex items-center justify-between text-sm"
          >
            <span class="text-foreground font-medium truncate">{{
              p.product_name
            }}</span>
            <span class="text-muted-foreground">{{ p.total_sold }} uds</span>
            <span class="font-semibold text-foreground">{{
              p.revenue.toFixed(2)
            }}</span>
          </div>
        </div>
      </div>

      <!-- Inventory remaining -->
      <div
        v-if="summary.inventory_remaining"
        class="rounded-xl border bg-card border-border shadow-sm px-6 py-4 flex items-center justify-between gap-4"
      >
        <div class="text-sm text-muted-foreground">
          <span class="font-semibold text-foreground">{{
            summary.inventory_remaining.low_stock_count
          }}</span>
          con stock bajo,
          <span class="font-semibold text-foreground">{{
            summary.inventory_remaining.out_of_stock_count
          }}</span>
          sin stock (de
          {{ summary.inventory_remaining.total_products }} productos)
        </div>
        <NuxtLink
          to="/inventory"
          class="flex items-center gap-1.5 text-sm font-semibold text-primary hover:underline shrink-0"
        >
          Ver inventario
          <ArrowRight class="w-3.5 h-3.5" />
        </NuxtLink>
      </div>
    </div>

    <CloseCashSessionModal v-model:open="closeModalOpen" @closed="onClosed" />
  </div>
</template>
