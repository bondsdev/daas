<script setup lang="ts">
import {
  ShoppingCart,
  CheckCircle,
  XCircle,
  Package,
  Loader2,
  AlertTriangle,
  Receipt,
  Save,
  Pencil,
} from "lucide-vue-next";

interface POLine {
  id: string;
  description: string;
  quantity_ordered: number;
  unit_cost: number;
  unit: string;
  subtotal: number;
}

interface Supplier {
  id: string;
  name: string;
  rif: string;
}

interface PurchaseOrder {
  id: string;
  status: "draft" | "ordered" | "received" | "cancelled";
  notes: string;
  currency: "VES" | "USD";
  total: number;
  ordered_at: string | null;
  received_at: string | null;
  created_at: string;
  lines: POLine[];
  supplier?: Supplier;
  supplier_invoice_number: string;
  supplier_invoice_date: string | null;
  supplier_invoice_tax_base: number;
  supplier_invoice_tax_amount: number;
}

interface InvoicePayload {
  invoice_number: string;
  invoice_date: string;
  tax_base: number;
  tax_amount: number;
}

const props = defineProps<{
  po: PurchaseOrder | null;
  loading: boolean;
  loadError: string;
  actionLoading: boolean;
  actionError: string;
  savingInvoice: boolean;
  invoiceError: string;
}>();

const emit = defineEmits<{
  order: [];
  receive: [];
  cancel: [];
  retry: [];
  saveInvoice: [payload: InvoicePayload];
}>();

const confirmingCancel = ref(false);

// ─── Factura del proveedor (Fase 1) ─────────────────────────────────────────
const editingInvoice = ref(false);
const invoiceForm = reactive({
  invoice_number: "",
  invoice_date: "",
  tax_base: 0,
  tax_amount: 0,
});
const invoiceTotal = computed(
  () => Number(invoiceForm.tax_base || 0) + Number(invoiceForm.tax_amount || 0),
);

function openInvoiceForm() {
  invoiceForm.invoice_number = props.po?.supplier_invoice_number ?? "";
  invoiceForm.invoice_date =
    props.po?.supplier_invoice_date?.slice(0, 10) ?? "";
  invoiceForm.tax_base = props.po?.supplier_invoice_tax_base ?? 0;
  invoiceForm.tax_amount = props.po?.supplier_invoice_tax_amount ?? 0;
  editingInvoice.value = true;
}

function submitInvoice() {
  emit("saveInvoice", {
    invoice_number: invoiceForm.invoice_number.trim(),
    invoice_date: invoiceForm.invoice_date || "",
    tax_base: Number(invoiceForm.tax_base) || 0,
    tax_amount: Number(invoiceForm.tax_amount) || 0,
  });
}

// El page hace la llamada API; cuando termina sin error, cerramos el form.
watch(
  () => props.savingInvoice,
  (now, prev) => {
    if (prev && !now && !props.invoiceError) editingInvoice.value = false;
  },
);

const STATUS: Record<string, { label: string; chip: string; icon: Component }> =
  {
    draft: {
      label: "Borrador",
      chip: "bg-gray-100 text-gray-600",
      icon: ShoppingCart,
    },
    ordered: {
      label: "Ordenada",
      chip: "bg-blue-100 text-blue-700",
      icon: Package,
    },
    received: {
      label: "Recibida",
      chip: "bg-emerald-100 text-emerald-700",
      icon: CheckCircle,
    },
    cancelled: {
      label: "Cancelada",
      chip: "bg-rose-100 text-rose-600",
      icon: XCircle,
    },
  };

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d).toLocaleDateString("es-VE", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// For date-only fields (e.g. "2026-11-20", no time component). Parsing those
// through `new Date(str)` reads them as UTC midnight, which displays as the
// previous day in negative-UTC-offset zones (Venezuela, UTC-4). Building the
// Date from the split components uses the local-time constructor instead.
function fmtDateOnly(d: string | null | undefined) {
  if (!d) return "—";
  const [y, m, day] = d.slice(0, 10).split("-").map(Number);
  return new Date(y, m - 1, day).toLocaleDateString("es-VE", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

// Discrepancy trace (point 5): the PO total is frozen at creation and never
// auto-corrected. When an invoice is later recorded with a different amount,
// surface it as a display-only warning instead of silently losing the gap.
const invoiceRecorded = computed(() => !!props.po?.supplier_invoice_number);
const invoiceRecordedTotal = computed(
  () =>
    Number(props.po?.supplier_invoice_tax_base ?? 0) +
    Number(props.po?.supplier_invoice_tax_amount ?? 0),
);
const invoiceDiscrepancy = computed(() => {
  if (!props.po || !invoiceRecorded.value) return 0;
  return invoiceRecordedTotal.value - props.po.total;
});

const hasActionBar = computed(
  () => props.po && !["received", "cancelled"].includes(props.po.status),
);
</script>

<template>
  <MobileScreen title="Orden de compra" back subtitle="Proveedores">
    <div v-if="loading" class="flex justify-center py-20">
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
    </div>

    <div v-else-if="loadError" class="px-4 py-12 text-center">
      <AlertTriangle class="mx-auto mb-3 size-10 text-destructive" />
      <p class="font-medium text-destructive">{{ loadError }}</p>
      <button
        type="button"
        class="mt-4 h-10 rounded-xl border border-border px-5 text-sm font-semibold text-muted-foreground active:bg-accent"
        @click="emit('retry')"
      >
        Reintentar
      </button>
    </div>

    <template v-else-if="po">
      <!-- Hero -->
      <div class="px-4 pt-4">
        <div class="rounded-2xl border border-border bg-white p-5 space-y-3">
          <div class="flex items-center justify-between gap-2">
            <span
              class="inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-bold"
              :class="STATUS[po.status]?.chip"
            >
              <component :is="STATUS[po.status]?.icon" class="size-3.5" />
              {{ STATUS[po.status]?.label }}
            </span>
            <span class="font-mono text-xs text-muted-foreground">
              #{{ po.id.slice(0, 8) }}
            </span>
          </div>

          <div>
            <p class="text-2xl font-black tabular-nums text-foreground">
              {{ fmtCurrency(po.total) }} {{ po.currency ?? "VES" }}
            </p>
            <p class="mt-1 text-sm font-semibold text-foreground">
              {{ po.supplier?.name ?? "Proveedor desconocido" }}
            </p>
            <p
              v-if="po.supplier?.rif"
              class="font-mono text-xs text-muted-foreground"
            >
              RIF: {{ po.supplier.rif }}
            </p>
            <NuxtLink
              v-if="po.status === 'draft'"
              :to="`/suppliers/purchase-orders?edit=${po.id}`"
              class="mt-2 inline-flex items-center gap-1.5 text-sm font-semibold text-primary active:opacity-70"
            >
              <Pencil class="size-3.5" /> Editar orden
            </NuxtLink>
          </div>

          <div
            class="grid grid-cols-3 gap-2 pt-2 border-t border-border text-xs"
          >
            <div>
              <p class="text-muted-foreground">Creada</p>
              <p class="font-medium text-foreground">
                {{ fmtDate(po.created_at) }}
              </p>
            </div>
            <div>
              <p class="text-muted-foreground">Ordenada</p>
              <p class="font-medium text-foreground">
                {{ fmtDate(po.ordered_at) }}
              </p>
            </div>
            <div>
              <p class="text-muted-foreground">Recibida</p>
              <p class="font-medium text-foreground">
                {{ fmtDate(po.received_at) }}
              </p>
            </div>
          </div>
        </div>
      </div>

      <!-- Notes -->
      <div v-if="po.notes" class="px-4 pt-4">
        <p class="rounded-2xl bg-muted/60 p-3.5 text-sm text-muted-foreground">
          {{ po.notes }}
        </p>
      </div>

      <!-- Action error -->
      <div v-if="actionError" class="px-4 pt-3">
        <div
          class="flex items-center gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive"
        >
          <AlertTriangle class="size-4 shrink-0" />
          {{ actionError }}
        </div>
      </div>

      <!-- Discrepancy trace (point 5): display-only, never auto-corrects total -->
      <div v-if="invoiceRecorded && invoiceDiscrepancy !== 0" class="px-4 pt-3">
        <div
          class="flex items-start gap-2 rounded-xl border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-800"
        >
          <AlertTriangle class="size-4 shrink-0 mt-0.5" />
          <span>
            Desvío con la factura:
            <strong
              >{{ invoiceDiscrepancy > 0 ? "+" : ""
              }}{{ fmtCurrency(invoiceDiscrepancy) }}
              {{ po.currency ?? "VES" }}</strong
            >. El total de la orden no cambia automáticamente.
          </span>
        </div>
      </div>

      <!-- Lines -->
      <MobileSectionHeader
        :title="`Líneas (${po.lines?.length ?? 0})`"
        class="pt-5"
      />
      <div class="divide-y divide-border border-y border-border bg-white">
        <div
          v-for="line in po.lines"
          :key="line.id"
          class="flex items-start gap-3 px-4 py-3"
        >
          <span
            class="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-xs font-bold text-muted-foreground"
          >
            {{ line.quantity_ordered }}×
          </span>
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium text-foreground">
              {{ line.description }}
            </p>
            <p class="text-[11px] text-muted-foreground">
              {{ line.unit }} · {{ fmtCurrency(line.unit_cost) }} c/u
            </p>
          </div>
          <span
            class="font-mono text-sm font-bold tabular-nums text-foreground"
          >
            {{ fmtCurrency(line.subtotal) }}
          </span>
        </div>

        <div class="flex items-center justify-between px-4 py-3.5">
          <span class="text-sm font-bold text-foreground">Total</span>
          <span class="font-mono text-lg font-black tabular-nums text-primary">
            {{ fmtCurrency(po.total) }} {{ po.currency ?? "VES" }}
          </span>
        </div>
      </div>

      <!-- Factura del proveedor (Fase 1) -->
      <MobileSectionHeader title="Factura del proveedor" class="pt-5" />
      <div class="border-y border-border bg-white px-4 py-4">
        <!-- Error -->
        <div
          v-if="invoiceError && editingInvoice"
          class="mb-3 rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {{ invoiceError }}
        </div>

        <!-- Vista: factura registrada -->
        <div
          v-if="!editingInvoice && po.supplier_invoice_number"
          class="space-y-3"
        >
          <div class="grid grid-cols-2 gap-3 text-sm">
            <div>
              <p class="text-[11px] text-muted-foreground">N° de factura</p>
              <p class="font-mono font-semibold text-foreground">
                {{ po.supplier_invoice_number }}
              </p>
            </div>
            <div>
              <p class="text-[11px] text-muted-foreground">Fecha</p>
              <p class="font-medium text-foreground">
                {{ fmtDateOnly(po.supplier_invoice_date) }}
              </p>
            </div>
            <div>
              <p class="text-[11px] text-muted-foreground">Base imponible</p>
              <p class="font-mono font-medium text-foreground">
                {{ fmtCurrency(po.supplier_invoice_tax_base) }}
              </p>
            </div>
            <div>
              <p class="text-[11px] text-muted-foreground">IVA (crédito)</p>
              <p class="font-mono font-semibold text-primary">
                {{ fmtCurrency(po.supplier_invoice_tax_amount) }}
              </p>
            </div>
          </div>
          <button
            v-if="po.status !== 'cancelled'"
            type="button"
            class="flex items-center gap-1.5 text-sm font-semibold text-primary active:opacity-70"
            @click="openInvoiceForm"
          >
            <Pencil class="size-3.5" /> Editar factura
          </button>
        </div>

        <!-- Vista: sin factura -->
        <div
          v-else-if="!editingInvoice"
          class="flex flex-col items-start gap-3"
        >
          <p class="text-sm text-muted-foreground">
            Registrá la factura del proveedor para declarar esta compra (crédito
            fiscal).
          </p>
          <button
            v-if="po.status !== 'cancelled'"
            type="button"
            class="no-min-tap flex h-11 items-center gap-2 rounded-2xl bg-primary px-5 text-sm font-bold text-white active:opacity-90"
            @click="openInvoiceForm"
          >
            <Receipt class="size-4" /> Registrar factura
          </button>
        </div>

        <!-- Formulario -->
        <div v-else class="space-y-3">
          <div>
            <label
              class="mb-1 block text-[11px] font-semibold text-muted-foreground"
              >N° de factura *</label
            >
            <input
              v-model="invoiceForm.invoice_number"
              placeholder="Ej. 00-00012345"
              class="h-11 w-full rounded-xl border border-border bg-white px-3 text-sm focus:border-primary focus:outline-none"
            />
          </div>
          <div>
            <label
              class="mb-1 block text-[11px] font-semibold text-muted-foreground"
              >Fecha de la factura</label
            >
            <input
              v-model="invoiceForm.invoice_date"
              type="date"
              class="h-11 w-full rounded-xl border border-border bg-white px-3 text-sm focus:border-primary focus:outline-none"
            />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label
                class="mb-1 block text-[11px] font-semibold text-muted-foreground"
                >Base imponible</label
              >
              <input
                v-model="invoiceForm.tax_base"
                type="number"
                inputmode="decimal"
                min="0"
                step="any"
                class="h-11 w-full rounded-xl border border-border bg-white px-3 font-mono text-sm focus:border-primary focus:outline-none"
              />
            </div>
            <div>
              <label
                class="mb-1 block text-[11px] font-semibold text-muted-foreground"
                >IVA (crédito)</label
              >
              <input
                v-model="invoiceForm.tax_amount"
                type="number"
                inputmode="decimal"
                min="0"
                step="any"
                class="h-11 w-full rounded-xl border border-border bg-white px-3 font-mono text-sm focus:border-primary focus:outline-none"
              />
            </div>
          </div>
          <div
            class="flex items-center justify-between rounded-xl bg-primary/5 px-3 py-2.5"
          >
            <span class="text-sm font-medium text-foreground"
              >Total factura</span
            >
            <span class="font-mono font-bold text-primary">{{
              fmtCurrency(invoiceTotal)
            }}</span>
          </div>
          <div class="flex gap-2 pt-1">
            <button
              type="button"
              :disabled="savingInvoice"
              class="no-min-tap h-12 flex-1 rounded-2xl bg-primary text-sm font-bold text-white active:opacity-90 disabled:opacity-50"
              @click="submitInvoice"
            >
              <Loader2
                v-if="savingInvoice"
                class="mx-auto size-5 animate-spin"
              />
              <span v-else class="flex items-center justify-center gap-2">
                <Save class="size-4" /> Guardar
              </span>
            </button>
            <button
              type="button"
              class="no-min-tap h-12 rounded-2xl border border-border px-5 text-sm font-bold text-muted-foreground active:bg-accent"
              @click="editingInvoice = false"
            >
              Cancelar
            </button>
          </div>
        </div>
      </div>

      <!-- Received confirmation -->
      <div v-if="po.status === 'received'" class="px-4 pt-4">
        <p class="flex items-center gap-2 text-sm text-emerald-600">
          <CheckCircle class="size-4" />
          Recibida el {{ fmtDate(po.received_at) }} — inventario actualizado
        </p>
      </div>

      <div v-if="po.status === 'cancelled'" class="px-4 pt-4">
        <p class="flex items-center gap-2 text-sm text-rose-600">
          <XCircle class="size-4" />
          Orden cancelada
        </p>
      </div>

      <!-- Spacer -->
      <div v-if="hasActionBar" class="h-24" />
    </template>

    <!-- Action bar -->
    <template v-if="hasActionBar && po">
      <div
        class="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-white px-4 pb-[calc(0.75rem+env(safe-area-inset-bottom))] pt-3"
      >
        <!-- Confirming cancel -->
        <div v-if="confirmingCancel" class="flex gap-2">
          <button
            type="button"
            :disabled="actionLoading"
            class="no-min-tap h-12 flex-1 rounded-2xl bg-red-500 text-sm font-bold text-white active:bg-red-600 disabled:opacity-50"
            @click="emit('cancel')"
          >
            <Loader2 v-if="actionLoading" class="mx-auto size-5 animate-spin" />
            <span v-else>Sí, cancelar</span>
          </button>
          <button
            type="button"
            class="no-min-tap h-12 rounded-2xl border border-border px-5 text-sm font-bold text-muted-foreground active:bg-accent"
            @click="confirmingCancel = false"
          >
            No
          </button>
        </div>

        <div v-else class="flex gap-2">
          <!-- Cancel icon -->
          <button
            type="button"
            class="no-min-tap h-12 w-12 shrink-0 rounded-2xl border border-rose-200 text-rose-500 active:bg-rose-50"
            aria-label="Cancelar orden"
            @click="confirmingCancel = true"
          >
            <XCircle class="mx-auto size-5" />
          </button>

          <!-- draft → order -->
          <button
            v-if="po.status === 'draft'"
            type="button"
            :disabled="actionLoading"
            class="no-min-tap h-12 flex-1 rounded-2xl bg-blue-600 text-sm font-bold text-white active:opacity-90 disabled:opacity-50"
            @click="emit('order')"
          >
            <Loader2 v-if="actionLoading" class="mx-auto size-5 animate-spin" />
            <span v-else class="flex items-center justify-center gap-2">
              <Package class="size-4" /> Confirmar orden
            </span>
          </button>

          <!-- ordered → receive -->
          <button
            v-else-if="po.status === 'ordered'"
            type="button"
            :disabled="actionLoading"
            class="no-min-tap h-12 flex-1 rounded-2xl bg-emerald-600 text-sm font-bold text-white active:opacity-90 disabled:opacity-50"
            @click="emit('receive')"
          >
            <Loader2 v-if="actionLoading" class="mx-auto size-5 animate-spin" />
            <span v-else class="flex items-center justify-center gap-2">
              <CheckCircle class="size-4" /> Marcar recibida
            </span>
          </button>
        </div>
      </div>
    </template>
  </MobileScreen>
</template>
