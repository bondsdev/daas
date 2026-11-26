<script setup lang="ts">
import {
  ShoppingCart,
  Plus,
  Search,
  X,
  Save,
  Trash2,
  Loader2,
  FolderOpen,
  ChevronRight,
  Pencil,
} from "lucide-vue-next";
import { Sheet, SheetHeader, SheetFooter } from "~/components/ui/sheet";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { useApiFetch } from "~/composables/useAuth";
import type { Supplier } from "~/components/suppliers/SupplierFormModal.vue";
import type { Product } from "~/components/products/ProductFormModal.vue";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

// ─── Types ────────────────────────────────────────────────────────────────────

interface POLine {
  id: string;
  product_id: string;
  description: string;
  quantity_ordered: number;
  unit_cost: number;
  unit: string;
  subtotal: number;
}

export interface PurchaseOrder {
  id: string;
  supplier_id: string;
  status: "draft" | "ordered" | "received" | "cancelled";
  notes: string;
  currency: "VES" | "USD";
  total: number;
  ordered_at: string | null;
  received_at: string | null;
  created_at: string;
  lines: POLine[];
  supplier?: Supplier;
  supplier_invoice_number?: string;
  supplier_invoice_date?: string | null;
  supplier_invoice_tax_base?: number;
  supplier_invoice_tax_amount?: number;
}

// ─── State ───────────────────────────────────────────────────────────────────
const { isMobile } = useMobileMode();
const route = useRoute();
const router = useRouter();

const orders = ref<PurchaseOrder[]>([]);
const suppliers = ref<Supplier[]>([]);
const products = ref<Product[]>([]);
const loading = ref(false);
const loadError = ref("");
const searchQuery = ref("");
const statusFilter = ref("all");
const formOpen = ref(false);
const saving = ref(false);
const saveError = ref("");

// ─── Form state ──────────────────────────────────────────────────────────────
interface FormLine {
  product_id: string;
  description: string;
  quantity: number;
  unit_cost: number;
  unit: string;
}

// `editingId` is set when the sheet is editing an existing draft order
// (PUT) instead of creating a new one (POST).
const editingId = ref<string | null>(null);

const form = reactive({
  supplier_id: "",
  notes: "",
  currency: "VES" as "VES" | "USD",
  lines: [] as FormLine[],
});

function addLine() {
  form.lines.push({
    product_id: "",
    description: "",
    quantity: 1,
    unit_cost: 0,
    unit: "",
  });
}

function removeLine(idx: number) {
  form.lines.splice(idx, 1);
}

function onProductChange(line: FormLine) {
  const product = products.value.find((p) => p.id === line.product_id);
  if (product && !line.description) {
    line.description = product.name;
  }
  if (product && !line.unit) {
    line.unit = product.unit ?? "";
  }
}

const formTotal = computed(() =>
  form.lines.reduce((acc, l) => acc + l.quantity * l.unit_cost, 0),
);

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d).toLocaleDateString("es-VE", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

// ─── Filters ─────────────────────────────────────────────────────────────────
const STATUS_CONFIG: Record<string, { label: string; class: string }> = {
  draft: { label: "Borrador", class: "bg-muted text-muted-foreground" },
  ordered: {
    label: "Ordenada",
    class: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
  },
  received: {
    label: "Recibida",
    class:
      "bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300",
  },
  cancelled: {
    label: "Cancelada",
    class: "bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400",
  },
};

const filterTabs = [
  { key: "all", label: "Todas" },
  { key: "draft", label: "Borradores" },
  { key: "ordered", label: "Ordenadas" },
  { key: "received", label: "Recibidas" },
  { key: "cancelled", label: "Canceladas" },
];

const filteredOrders = computed(() => {
  let list = orders.value;
  if (statusFilter.value !== "all") {
    list = list.filter((o) => o.status === statusFilter.value);
  }
  if (searchQuery.value.trim()) {
    const q = searchQuery.value.toLowerCase();
    list = list.filter(
      (o) =>
        o.supplier?.name?.toLowerCase().includes(q) ||
        o.id.toLowerCase().includes(q),
    );
  }
  return list;
});

const counts = computed(() => ({
  draft: orders.value.filter((o) => o.status === "draft").length,
  ordered: orders.value.filter((o) => o.status === "ordered").length,
  received: orders.value.filter((o) => o.status === "received").length,
}));

// ─── API ─────────────────────────────────────────────────────────────────────
async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const [pos, sups, prods] = await Promise.all([
      useApiFetch<PurchaseOrder[]>("/api/v1/purchase-orders"),
      useApiFetch<Supplier[]>("/api/v1/suppliers"),
      useApiFetch<Product[]>("/api/v1/products"),
    ]);
    orders.value = pos;
    suppliers.value = sups.filter((s) => s.active);
    products.value = prods.filter((p) => p.active);
  } catch {
    loadError.value = "No se pudieron cargar las órdenes de compra";
  } finally {
    loading.value = false;
  }
}

function openForm() {
  editingId.value = null;
  form.supplier_id = "";
  form.notes = "";
  form.currency = "VES";
  form.lines = [];
  addLine();
  saveError.value = "";
  formOpen.value = true;
}

// Prefills the create-form sheet from an existing draft order and switches
// submission to PUT (updatePO) instead of POST.
function openEditForm(order: PurchaseOrder) {
  editingId.value = order.id;
  form.supplier_id = order.supplier_id;
  form.notes = order.notes ?? "";
  form.currency = order.currency ?? "VES";
  form.lines = (order.lines ?? []).map((l) => ({
    product_id: l.product_id,
    description: l.description,
    quantity: l.quantity_ordered,
    unit_cost: l.unit_cost,
    unit: l.unit ?? "",
  }));
  if (form.lines.length === 0) addLine();
  saveError.value = "";
  formOpen.value = true;
}

function validateForm(): boolean {
  if (!form.supplier_id) {
    saveError.value = "Seleccioná un proveedor";
    return false;
  }
  if (form.lines.length === 0) {
    saveError.value = "Agregá al menos una línea";
    return false;
  }
  for (const l of form.lines) {
    if (!l.product_id) {
      saveError.value = "Seleccioná un producto en cada línea";
      return false;
    }
    if (l.quantity <= 0 || l.unit_cost <= 0) {
      saveError.value = "Cantidad y costo deben ser mayores a 0";
      return false;
    }
  }
  return true;
}

function buildPOBody() {
  return {
    supplier_id: form.supplier_id,
    notes: form.notes,
    currency: form.currency,
    lines: form.lines.map((l) => ({
      product_id: l.product_id,
      description: l.description,
      quantity: l.quantity,
      unit_cost: l.unit_cost,
      ...(l.unit ? { unit: l.unit } : {}),
    })),
  };
}

async function createPO() {
  if (!validateForm()) return;

  saving.value = true;
  saveError.value = "";
  try {
    const po = await useApiFetch<PurchaseOrder>("/api/v1/purchase-orders", {
      method: "POST",
      body: buildPOBody(),
    });
    orders.value.unshift(po);
    formOpen.value = false;
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    saveError.value = e?.data?.detail ?? "Error al crear la orden";
  } finally {
    saving.value = false;
  }
}

async function updatePO() {
  if (!editingId.value || !validateForm()) return;

  saving.value = true;
  saveError.value = "";
  try {
    const po = await useApiFetch<PurchaseOrder>(
      `/api/v1/purchase-orders/${editingId.value}`,
      {
        method: "PUT",
        body: buildPOBody(),
      },
    );
    const idx = orders.value.findIndex((o) => o.id === po.id);
    if (idx !== -1) {
      orders.value[idx] = po;
    } else {
      orders.value.unshift(po);
    }
    formOpen.value = false;
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    saveError.value = e?.data?.detail ?? "Error al actualizar la orden";
  } finally {
    saving.value = false;
  }
}

function submitForm() {
  return editingId.value ? updatePO() : createPO();
}

onMounted(async () => {
  await load();
  // Deep-link from the detail page's "Editar" button (`?edit=<id>`).
  const editId = route.query.edit;
  if (typeof editId === "string") {
    const target = orders.value.find((o) => o.id === editId);
    if (target) openEditForm(target);
    router.replace({ query: { ...route.query, edit: undefined } });
  }
});
</script>

<template>
  <!-- MOBILE -->
  <MobileScreensPurchaseOrders
    v-if="isMobile"
    :orders="orders"
    :loading="loading"
    :load-error="loadError"
    @create="openForm"
    @retry="load"
  />

  <!-- WEB -->
  <div v-else class="p-4 sm:p-6 space-y-6">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">Órdenes de compra</h1>
        <p class="text-sm text-muted-foreground mt-0.5">
          {{ counts.ordered }} ordenada{{ counts.ordered !== 1 ? "s" : "" }} ·
          {{ counts.draft }} borrador{{ counts.draft !== 1 ? "es" : "" }}
        </p>
      </div>
      <button
        class="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
        @click="openForm"
      >
        <Plus class="w-4 h-4" />
        Nueva orden
      </button>
    </div>

    <!-- Stats -->
    <div class="grid grid-cols-3 gap-4">
      <div class="border bg-card rounded-xl p-4">
        <p class="text-sm text-muted-foreground">Borradores</p>
        <p class="text-2xl font-bold text-foreground mt-1">
          {{ counts.draft }}
        </p>
      </div>
      <div class="border bg-card rounded-xl p-4">
        <p class="text-sm text-muted-foreground">Ordenadas</p>
        <p class="text-2xl font-bold text-blue-600 mt-1">
          {{ counts.ordered }}
        </p>
      </div>
      <div class="border bg-card rounded-xl p-4">
        <p class="text-sm text-muted-foreground">Recibidas</p>
        <p class="text-2xl font-bold text-emerald-600 mt-1">
          {{ counts.received }}
        </p>
      </div>
    </div>

    <!-- Filters -->
    <div class="flex items-center gap-3 flex-wrap">
      <div class="relative flex-1 min-w-48">
        <Search
          class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground"
        />
        <input
          v-model="searchQuery"
          class="w-full h-10 pl-9 pr-3 rounded-lg border border-input bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/20"
          placeholder="Buscar por proveedor..."
        />
      </div>
      <div class="flex gap-1 bg-muted/50 rounded-lg p-1 overflow-x-auto">
        <button
          v-for="tab in filterTabs"
          :key="tab.key"
          :class="[
            'px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
            statusFilter === tab.key
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          ]"
          @click="statusFilter = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>
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
      Cargando órdenes...
    </div>

    <!-- Empty -->
    <div
      v-else-if="!loading && filteredOrders.length === 0"
      class="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3"
    >
      <FolderOpen class="w-12 h-12 opacity-30" />
      <p class="text-sm">
        {{
          searchQuery || statusFilter !== "all"
            ? "No hay órdenes que coincidan con el filtro"
            : "Aún no hay órdenes de compra"
        }}
      </p>
      <button
        v-if="!searchQuery && statusFilter === 'all'"
        class="text-sm text-primary hover:underline"
        @click="openForm"
      >
        Crear la primera orden
      </button>
    </div>

    <!-- Table -->
    <div v-else-if="!loading" class="border bg-card rounded-xl overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-border bg-muted/30">
              <th class="px-4 py-3 text-left font-medium text-muted-foreground">
                Proveedor
              </th>
              <th class="px-4 py-3 text-left font-medium text-muted-foreground">
                Estado
              </th>
              <th
                class="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell"
              >
                Fecha
              </th>
              <th
                class="px-4 py-3 text-right font-medium text-muted-foreground"
              >
                Total
              </th>
              <th class="px-4 py-3" />
            </tr>
          </thead>
          <tbody class="divide-y divide-border">
            <tr
              v-for="po in filteredOrders"
              :key="po.id"
              class="hover:bg-muted/20 transition-colors group"
            >
              <td class="px-4 py-3">
                <p class="font-medium text-foreground">
                  {{ po.supplier?.name ?? "—" }}
                </p>
                <p
                  v-if="po.notes"
                  class="text-xs text-muted-foreground truncate max-w-48"
                >
                  {{ po.notes }}
                </p>
              </td>
              <td class="px-4 py-3">
                <span
                  :class="[
                    'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium',
                    STATUS_CONFIG[po.status]?.class,
                  ]"
                >
                  {{ STATUS_CONFIG[po.status]?.label }}
                </span>
              </td>
              <td
                class="px-4 py-3 text-muted-foreground hidden md:table-cell text-xs"
              >
                {{ fmtDate(po.created_at) }}
              </td>
              <td
                class="px-4 py-3 text-right font-mono font-medium text-foreground"
              >
                {{ fmtCurrency(po.total) }} {{ po.currency ?? "VES" }}
              </td>
              <td class="px-4 py-3 text-right">
                <div
                  class="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-all"
                >
                  <button
                    v-if="po.status === 'draft'"
                    class="p-1.5 rounded-lg hover:bg-muted transition-all text-muted-foreground inline-flex"
                    title="Editar orden"
                    @click="openEditForm(po)"
                  >
                    <Pencil class="w-4 h-4" />
                  </button>
                  <NuxtLink
                    :to="`/suppliers/purchase-orders/${po.id}`"
                    class="p-1.5 rounded-lg hover:bg-muted transition-all text-muted-foreground inline-flex"
                  >
                    <ChevronRight class="w-4 h-4" />
                  </NuxtLink>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- New PO slide-over -->
    <Sheet v-model:open="formOpen">
      <SheetHeader>
        <div class="flex items-center gap-3">
          <div class="p-2 bg-primary/10 rounded-lg">
            <ShoppingCart class="w-5 h-5 text-primary" />
          </div>
          <div>
            <h2 class="text-base font-semibold text-foreground">
              {{
                editingId ? "Editar orden de compra" : "Nueva orden de compra"
              }}
            </h2>
            <p class="text-xs text-muted-foreground mt-0.5">
              Completá los datos del pedido al proveedor
            </p>
          </div>
        </div>
        <button
          class="p-1.5 rounded-lg hover:bg-gray-100 transition-colors text-muted-foreground"
          @click="formOpen = false"
        >
          <X class="w-4 h-4" />
        </button>
      </SheetHeader>

      <!-- Body -->
      <div class="flex-1 overflow-y-auto px-6 py-5 space-y-5">
        <div
          v-if="saveError"
          class="bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg px-4 py-3"
        >
          {{ saveError }}
        </div>

        <!-- Supplier + currency -->
        <div class="grid grid-cols-3 gap-3">
          <div class="col-span-2 space-y-1.5">
            <label class="text-sm font-medium text-foreground"
              >Proveedor <span class="text-red-500">*</span></label
            >
            <select
              v-model="form.supplier_id"
              class="w-full h-11 px-3 border border-input bg-white rounded-lg text-sm focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
            >
              <option value="">Seleccionar proveedor...</option>
              <option v-for="sup in suppliers" :key="sup.id" :value="sup.id">
                {{ sup.name }}
              </option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label class="text-sm font-medium text-foreground">Moneda</label>
            <select
              v-model="form.currency"
              class="w-full h-11 px-3 border border-input bg-white rounded-lg text-sm focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
            >
              <option value="VES">VES</option>
              <option value="USD">USD</option>
            </select>
          </div>
        </div>

        <!-- Notes -->
        <div class="space-y-1.5">
          <label class="text-sm font-medium text-foreground">Notas</label>
          <textarea
            v-model="form.notes"
            rows="2"
            class="w-full px-3 py-2 border border-input bg-white rounded-lg text-sm focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 resize-none"
            placeholder="Observaciones de la orden..."
          />
        </div>

        <!-- Lines -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="text-sm font-medium text-foreground"
              >Líneas <span class="text-red-500">*</span></label
            >
            <button
              class="text-xs text-primary hover:underline flex items-center gap-1"
              @click="addLine"
            >
              <Plus class="w-3 h-3" />
              Agregar línea
            </button>
          </div>

          <div
            v-for="(line, idx) in form.lines"
            :key="idx"
            class="bg-gray-50 border border-border rounded-lg p-4 space-y-3"
          >
            <div class="flex items-center justify-between">
              <span class="text-xs font-medium text-muted-foreground"
                >Línea {{ idx + 1 }}</span
              >
              <button
                class="p-1 rounded hover:bg-gray-100 text-muted-foreground transition-colors"
                @click="removeLine(idx)"
              >
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>

            <div class="space-y-1.5">
              <label class="text-xs font-medium text-muted-foreground"
                >Producto</label
              >
              <select
                v-model="line.product_id"
                class="w-full h-9 px-3 border border-input bg-white rounded-lg text-sm focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
                @change="onProductChange(line)"
              >
                <option value="">Seleccionar producto...</option>
                <option
                  v-for="prod in products"
                  :key="prod.id"
                  :value="prod.id"
                >
                  {{ prod.name }}
                </option>
              </select>
            </div>

            <div class="space-y-1.5">
              <label class="text-xs font-medium text-muted-foreground"
                >Descripción</label
              >
              <Input
                v-model="line.description"
                class="h-9"
                placeholder="Descripción en la orden..."
              />
            </div>

            <div class="grid grid-cols-3 gap-3">
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >Cantidad</label
                >
                <Input
                  v-model="line.quantity"
                  type="number"
                  min="0.001"
                  step="any"
                  class="h-9"
                />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >Unidad</label
                >
                <Input v-model="line.unit" class="h-9" placeholder="unidad" />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-medium text-muted-foreground"
                  >Costo unitario</label
                >
                <Input
                  v-model="line.unit_cost"
                  type="number"
                  min="0"
                  step="any"
                  class="h-9"
                />
              </div>
            </div>

            <div class="text-right text-xs text-muted-foreground">
              Subtotal:
              <span class="font-mono font-medium text-foreground"
                >{{ fmtCurrency(line.quantity * line.unit_cost) }}
                {{ form.currency }}</span
              >
            </div>
          </div>

          <!-- Total -->
          <div
            v-if="form.lines.length > 0"
            class="flex items-center justify-between px-4 py-3 bg-primary/5 rounded-lg border border-primary/20"
          >
            <span class="text-sm font-medium text-foreground">Total</span>
            <span class="font-mono font-bold text-primary text-lg"
              >{{ fmtCurrency(formTotal) }} {{ form.currency }}</span
            >
          </div>
        </div>
      </div>

      <SheetFooter>
        <Button variant="outline" @click="formOpen = false">Cancelar</Button>
        <Button :disabled="saving" @click="submitForm">
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          <Save v-else class="w-4 h-4" />
          {{
            saving
              ? editingId
                ? "Guardando..."
                : "Creando..."
              : editingId
                ? "Guardar cambios"
                : "Crear orden"
          }}
        </Button>
      </SheetFooter>
    </Sheet>
  </div>
</template>
