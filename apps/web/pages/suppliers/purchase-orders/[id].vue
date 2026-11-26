<script setup lang="ts">
import {
  ArrowLeft,
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
import { useApiFetch } from "~/composables/useAuth";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

interface POLine {
  id: string;
  product_id: string;
  description: string;
  quantity_ordered: number;
  unit_cost: number;
  unit: string;
  subtotal: number;
  sort_order: number;
}

interface Supplier {
  id: string;
  name: string;
  rif: string;
  contact_name: string;
  email: string;
  phone: string;
}

interface PurchaseOrder {
  id: string;
  supplier_id: string;
  status: "draft" | "ordered" | "received" | "cancelled";
  notes: string;
  currency: "VES" | "USD";
  total: number;
  ordered_at: string | null;
  received_at: string | null;
  created_at: string;
  updated_at: string;
  lines: POLine[];
  supplier?: Supplier;
  supplier_invoice_number: string;
  supplier_invoice_date: string | null;
  supplier_invoice_tax_base: number;
  supplier_invoice_tax_amount: number;
}

const { isMobile } = useMobileMode();
const route = useRoute();
const poId = route.params.id as string;

const po = ref<PurchaseOrder | null>(null);
const loading = ref(false);
const loadError = ref("");
const actionLoading = ref(false);
const actionError = ref("");
const confirmCancel = ref(false);

const STATUS_CONFIG: Record<
  string,
  { label: string; class: string; icon: Component }
> = {
  draft: {
    label: "Borrador",
    class: "bg-muted text-muted-foreground",
    icon: ShoppingCart,
  },
  ordered: {
    label: "Ordenada",
    class: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
    icon: Package,
  },
  received: {
    label: "Recibida",
    class:
      "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400",
    icon: CheckCircle,
  },
  cancelled: {
    label: "Cancelada",
    class: "bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400",
    icon: XCircle,
  },
};

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d).toLocaleDateString("es-VE", {
    day: "2-digit",
    month: "long",
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
    month: "long",
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
const invoiceRecorded = computed(() => !!po.value?.supplier_invoice_number);
const invoiceTotal_ = computed(
  () =>
    Number(po.value?.supplier_invoice_tax_base ?? 0) +
    Number(po.value?.supplier_invoice_tax_amount ?? 0),
);
const invoiceDiscrepancy = computed(() => {
  if (!po.value || !invoiceRecorded.value) return 0;
  return invoiceTotal_.value - po.value.total;
});

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    po.value = await useApiFetch<PurchaseOrder>(
      `/api/v1/purchase-orders/${poId}`,
    );
  } catch {
    loadError.value = "No se pudo cargar la orden de compra";
  } finally {
    loading.value = false;
  }
}

async function performAction(action: "order" | "receive" | "cancel") {
  actionLoading.value = true;
  actionError.value = "";
  confirmCancel.value = false;
  try {
    po.value = await useApiFetch<PurchaseOrder>(
      `/api/v1/purchase-orders/${poId}/${action}`,
      { method: "POST" },
    );
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    actionError.value = e?.data?.detail ?? "Ocurrió un error";
  } finally {
    actionLoading.value = false;
  }
}

// ─── Factura del proveedor (Fase 1) ─────────────────────────────────────────
const editingInvoice = ref(false);
const savingInvoice = ref(false);
const invoiceError = ref("");
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
  invoiceForm.invoice_number = po.value?.supplier_invoice_number ?? "";
  invoiceForm.invoice_date =
    po.value?.supplier_invoice_date?.slice(0, 10) ?? "";
  invoiceForm.tax_base = po.value?.supplier_invoice_tax_base ?? 0;
  invoiceForm.tax_amount = po.value?.supplier_invoice_tax_amount ?? 0;
  invoiceError.value = "";
  editingInvoice.value = true;
}

interface InvoicePayload {
  invoice_number: string;
  invoice_date: string;
  tax_base: number;
  tax_amount: number;
}

// Núcleo compartido web + mobile: hace la llamada al API.
async function persistInvoice(payload: InvoicePayload) {
  if (!payload.invoice_number.trim()) {
    invoiceError.value = "El número de factura es obligatorio";
    return;
  }
  savingInvoice.value = true;
  invoiceError.value = "";
  try {
    po.value = await useApiFetch<PurchaseOrder>(
      `/api/v1/purchase-orders/${poId}/invoice`,
      { method: "PUT", body: payload },
    );
    editingInvoice.value = false;
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    invoiceError.value = e?.data?.detail ?? "No se pudo guardar la factura";
  } finally {
    savingInvoice.value = false;
  }
}

// Wrapper para la vista web (arma el payload desde invoiceForm).
function saveInvoice() {
  persistInvoice({
    invoice_number: invoiceForm.invoice_number.trim(),
    invoice_date: invoiceForm.invoice_date || "",
    tax_base: Number(invoiceForm.tax_base) || 0,
    tax_amount: Number(invoiceForm.tax_amount) || 0,
  });
}

onMounted(load);
</script>

<template>
  <MobileScreensPurchaseOrderDetail
    v-if="isMobile"
    :po="po"
    :loading="loading"
    :load-error="loadError"
    :action-loading="actionLoading"
    :action-error="actionError"
    :saving-invoice="savingInvoice"
    :invoice-error="invoiceError"
    @order="performAction('order')"
    @receive="performAction('receive')"
    @cancel="performAction('cancel')"
    @retry="load"
    @save-invoice="persistInvoice"
  />
  <div v-else class="p-4 sm:p-6 space-y-6">
    <!-- Back -->
    <div class="flex items-center gap-3">
      <NuxtLink
        to="/suppliers/purchase-orders"
        class="p-2 rounded-lg hover:bg-muted transition-colors text-muted-foreground"
      >
        <ArrowLeft class="w-4 h-4" />
      </NuxtLink>
      <h1 class="text-xl font-bold text-foreground">Orden de compra</h1>
    </div>

    <!-- Error -->
    <div
      v-if="loadError"
      class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3"
    >
      {{ loadError }}
    </div>

    <!-- Loading -->
    <div
      v-if="loading"
      class="flex items-center justify-center py-20 text-muted-foreground"
    >
      <Loader2 class="w-6 h-6 animate-spin mr-2" />
      Cargando...
    </div>

    <template v-else-if="po">
      <!-- Header card -->
      <div class="border bg-card rounded-xl p-6 space-y-5">
        <!-- Status + supplier -->
        <div class="flex items-start justify-between gap-4 flex-wrap">
          <div>
            <div class="flex items-center gap-3 mb-2">
              <span
                :class="[
                  'inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm font-medium',
                  STATUS_CONFIG[po.status]?.class,
                ]"
              >
                <component
                  :is="STATUS_CONFIG[po.status]?.icon"
                  class="w-4 h-4"
                />
                {{ STATUS_CONFIG[po.status]?.label }}
              </span>
            </div>
            <p class="text-lg font-semibold text-foreground">
              {{ po.supplier?.name ?? "Proveedor desconocido" }}
            </p>
            <p
              v-if="po.supplier?.rif"
              class="text-sm text-muted-foreground font-mono"
            >
              RIF: {{ po.supplier.rif }}
            </p>
          </div>
          <div class="text-right">
            <p class="text-3xl font-bold text-foreground font-mono">
              {{ fmtCurrency(po.total) }} {{ po.currency ?? "VES" }}
            </p>
            <p class="text-sm text-muted-foreground mt-0.5">
              Total de la orden
            </p>
          </div>
        </div>

        <!-- Edit (draft only) -->
        <div v-if="po.status === 'draft'">
          <NuxtLink
            :to="`/suppliers/purchase-orders?edit=${po.id}`"
            class="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
          >
            <Pencil class="w-3.5 h-3.5" /> Editar orden
          </NuxtLink>
        </div>

        <!-- Discrepancy trace (point 5): PO total is never auto-corrected —
             this is a display-only alert when the recorded invoice differs. -->
        <div
          v-if="invoiceRecorded && invoiceDiscrepancy !== 0"
          class="flex items-start gap-2 p-3 rounded-lg border border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-400 text-sm"
        >
          <AlertTriangle class="w-4 h-4 shrink-0 mt-0.5" />
          <span>
            Hay un desvío entre la orden ({{ fmtCurrency(po.total) }}
            {{ po.currency ?? "VES" }}) y la factura registrada ({{
              fmtCurrency(invoiceTotal_)
            }}
            {{ po.currency ?? "VES" }}):
            <strong
              >{{ invoiceDiscrepancy > 0 ? "+" : ""
              }}{{ fmtCurrency(invoiceDiscrepancy) }}</strong
            >. El total original de la orden no se modifica automáticamente.
          </span>
        </div>

        <!-- Dates -->
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm">
          <div>
            <p class="text-muted-foreground text-xs">Creada</p>
            <p class="text-foreground font-medium">
              {{ fmtDate(po.created_at) }}
            </p>
          </div>
          <div>
            <p class="text-muted-foreground text-xs">Ordenada</p>
            <p class="text-foreground font-medium">
              {{ fmtDate(po.ordered_at) }}
            </p>
          </div>
          <div>
            <p class="text-muted-foreground text-xs">Recibida</p>
            <p class="text-foreground font-medium">
              {{ fmtDate(po.received_at) }}
            </p>
          </div>
        </div>

        <div
          v-if="po.notes"
          class="p-3 bg-muted/40 rounded-lg text-sm text-muted-foreground"
        >
          {{ po.notes }}
        </div>

        <!-- Action error -->
        <div
          v-if="actionError"
          class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3 flex items-center gap-2"
        >
          <AlertTriangle class="w-4 h-4 shrink-0" />
          {{ actionError }}
        </div>

        <!-- Actions -->
        <div
          v-if="po.status !== 'received' && po.status !== 'cancelled'"
          class="flex items-center gap-3 flex-wrap"
        >
          <!-- Order (draft → ordered) -->
          <button
            v-if="po.status === 'draft'"
            :disabled="actionLoading"
            class="flex items-center gap-2 px-4 py-2 text-sm rounded-lg bg-blue-600 text-white hover:bg-blue-700 transition-colors disabled:opacity-50"
            @click="performAction('order')"
          >
            <Loader2 v-if="actionLoading" class="w-4 h-4 animate-spin" />
            <Package v-else class="w-4 h-4" />
            Confirmar orden
          </button>

          <!-- Receive (ordered → received) -->
          <button
            v-if="po.status === 'ordered'"
            :disabled="actionLoading"
            class="flex items-center gap-2 px-4 py-2 text-sm rounded-lg bg-emerald-600 text-white hover:bg-emerald-700 transition-colors disabled:opacity-50"
            @click="performAction('receive')"
          >
            <Loader2 v-if="actionLoading" class="w-4 h-4 animate-spin" />
            <CheckCircle v-else class="w-4 h-4" />
            Marcar como recibida
          </button>

          <!-- Cancel -->
          <div v-if="!confirmCancel">
            <button
              :disabled="actionLoading"
              class="flex items-center gap-2 px-4 py-2 text-sm rounded-lg border border-destructive/50 text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
              @click="confirmCancel = true"
            >
              <XCircle class="w-4 h-4" />
              Cancelar orden
            </button>
          </div>
          <div v-else class="flex items-center gap-2">
            <span class="text-sm text-muted-foreground">¿Estás seguro?</span>
            <button
              :disabled="actionLoading"
              class="flex items-center gap-2 px-3 py-1.5 text-sm rounded-lg bg-destructive text-destructive-foreground hover:bg-destructive/90 transition-colors disabled:opacity-50"
              @click="performAction('cancel')"
            >
              <Loader2 v-if="actionLoading" class="w-3.5 h-3.5 animate-spin" />
              Sí, cancelar
            </button>
            <button
              class="px-3 py-1.5 text-sm rounded-lg border border-border hover:bg-muted transition-colors"
              @click="confirmCancel = false"
            >
              No
            </button>
          </div>
        </div>

        <!-- Received info -->
        <div
          v-if="po.status === 'received'"
          class="flex items-center gap-2 text-sm text-emerald-600 dark:text-emerald-400"
        >
          <CheckCircle class="w-4 h-4" />
          Recibida el {{ fmtDate(po.received_at) }} — inventario actualizado
          automáticamente
        </div>
      </div>

      <!-- Factura del proveedor (Fase 1) -->
      <div class="border bg-card rounded-xl overflow-hidden">
        <div
          class="px-6 py-4 border-b border-border flex items-center justify-between gap-3"
        >
          <div class="flex items-center gap-2">
            <Receipt class="w-4 h-4 text-primary" />
            <h3 class="font-semibold text-foreground">Factura del proveedor</h3>
          </div>
          <button
            v-if="
              !editingInvoice &&
              po.status !== 'cancelled' &&
              po.supplier_invoice_number
            "
            class="flex items-center gap-1.5 text-sm text-primary hover:underline"
            @click="openInvoiceForm"
          >
            <Pencil class="w-3.5 h-3.5" /> Editar
          </button>
        </div>

        <div class="p-6">
          <!-- Vista: factura registrada -->
          <div
            v-if="!editingInvoice && po.supplier_invoice_number"
            class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm"
          >
            <div>
              <p class="text-xs text-muted-foreground">N° de factura</p>
              <p class="font-semibold text-foreground font-mono">
                {{ po.supplier_invoice_number }}
              </p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground">Fecha</p>
              <p class="font-medium text-foreground">
                {{ fmtDateOnly(po.supplier_invoice_date) }}
              </p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground">Base imponible</p>
              <p class="font-medium text-foreground font-mono">
                {{ fmtCurrency(po.supplier_invoice_tax_base) }}
              </p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground">IVA (crédito fiscal)</p>
              <p class="font-semibold text-primary font-mono">
                {{ fmtCurrency(po.supplier_invoice_tax_amount) }}
              </p>
            </div>
          </div>

          <!-- Vista: sin factura aún -->
          <div
            v-else-if="!editingInvoice"
            class="flex flex-col items-start gap-3"
          >
            <p class="text-sm text-muted-foreground">
              Registrá la factura que te emitió el proveedor para poder declarar
              esta compra (crédito fiscal).
            </p>
            <button
              v-if="po.status !== 'cancelled'"
              class="flex items-center gap-2 px-4 py-2 text-sm rounded-lg bg-primary text-white hover:opacity-90 transition-opacity"
              @click="openInvoiceForm"
            >
              <Receipt class="w-4 h-4" /> Registrar factura
            </button>
          </div>

          <!-- Formulario -->
          <div v-else class="space-y-4">
            <div
              v-if="invoiceError"
              class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-2.5"
            >
              {{ invoiceError }}
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >N° de factura *</label
                >
                <input
                  v-model="invoiceForm.invoice_number"
                  placeholder="Ej. 00-00012345"
                  class="w-full h-10 px-3 border border-input rounded-lg text-sm bg-white focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
                />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >Fecha de la factura</label
                >
                <input
                  v-model="invoiceForm.invoice_date"
                  type="date"
                  class="w-full h-10 px-3 border border-input rounded-lg text-sm bg-white focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
                />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >Base imponible</label
                >
                <input
                  v-model="invoiceForm.tax_base"
                  type="number"
                  min="0"
                  step="any"
                  class="w-full h-10 px-3 border border-input rounded-lg text-sm bg-white focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 font-mono"
                />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >IVA (crédito fiscal)</label
                >
                <input
                  v-model="invoiceForm.tax_amount"
                  type="number"
                  min="0"
                  step="any"
                  class="w-full h-10 px-3 border border-input rounded-lg text-sm bg-white focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 font-mono"
                />
              </div>
            </div>
            <div
              class="flex items-center justify-between px-4 py-2.5 bg-primary/5 border border-primary/20 rounded-lg"
            >
              <span class="text-sm font-medium text-foreground"
                >Total factura</span
              >
              <span class="font-mono font-bold text-primary">{{
                fmtCurrency(invoiceTotal)
              }}</span>
            </div>
            <div class="flex items-center gap-2">
              <button
                :disabled="savingInvoice"
                class="flex items-center gap-2 px-4 py-2 text-sm rounded-lg bg-primary text-white hover:opacity-90 transition-opacity disabled:opacity-50"
                @click="saveInvoice"
              >
                <Loader2 v-if="savingInvoice" class="w-4 h-4 animate-spin" />
                <Save v-else class="w-4 h-4" />
                Guardar factura
              </button>
              <button
                class="px-4 py-2 text-sm rounded-lg border border-border hover:bg-muted transition-colors"
                @click="editingInvoice = false"
              >
                Cancelar
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Lines table -->
      <div class="border bg-card rounded-xl overflow-hidden">
        <div class="px-6 py-4 border-b border-border">
          <h3 class="font-semibold text-foreground">
            Líneas de la orden ({{ po.lines?.length ?? 0 }})
          </h3>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-border bg-muted/30">
                <th
                  class="px-4 py-3 text-left font-medium text-muted-foreground"
                >
                  Descripción
                </th>
                <th
                  class="px-4 py-3 text-right font-medium text-muted-foreground"
                >
                  Cantidad
                </th>
                <th
                  class="px-4 py-3 text-right font-medium text-muted-foreground hidden md:table-cell"
                >
                  Costo unit.
                </th>
                <th
                  class="px-4 py-3 text-right font-medium text-muted-foreground"
                >
                  Subtotal
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              <tr
                v-for="line in po.lines"
                :key="line.id"
                class="hover:bg-muted/20 transition-colors"
              >
                <td class="px-4 py-3 text-foreground">
                  {{ line.description }}
                </td>
                <td
                  class="px-4 py-3 text-right font-mono text-muted-foreground"
                >
                  {{ line.quantity_ordered }} {{ line.unit }}
                </td>
                <td
                  class="px-4 py-3 text-right font-mono text-muted-foreground hidden md:table-cell"
                >
                  {{ fmtCurrency(line.unit_cost) }}
                </td>
                <td
                  class="px-4 py-3 text-right font-mono font-medium text-foreground"
                >
                  {{ fmtCurrency(line.subtotal) }}
                </td>
              </tr>
            </tbody>
            <tfoot>
              <tr class="border-t-2 border-border bg-muted/20">
                <td
                  colspan="3"
                  class="px-4 py-3 text-right text-sm font-medium text-muted-foreground"
                >
                  Total
                </td>
                <td
                  class="px-4 py-3 text-right font-mono font-bold text-lg text-foreground"
                >
                  {{ fmtCurrency(po.total) }} {{ po.currency ?? "VES" }}
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>
