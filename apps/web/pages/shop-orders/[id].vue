<script setup lang="ts">
import {
  ArrowLeft,
  ShoppingCart,
  Clock,
  CreditCard,
  Package,
  CheckCircle2,
  XCircle,
  Mail,
  Phone,
  MapPin,
  AlertTriangle,
  Loader2,
  ExternalLink,
  Truck,
  Send,
  Paperclip,
  FileText,
  FileImage,
  Download,
  Bike,
  Zap,
  DollarSign,
} from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";
import { useAuthStore } from "~/stores/auth";
import { Select } from "~/components/ui/select";
import { Switch } from "~/components/ui/switch";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

interface OrderLine {
  id: string;
  product_id: string;
  name: string;
  unit_price: number;
  quantity: number;
  subtotal: number;
  is_fiscal: boolean;
  sort_order: number;
}

interface PaymentMethod {
  id: string;
  type: string;
  label: string;
  details: Record<string, string>;
  currency: string;
  active: boolean;
  sort_order: number;
}

interface ShopOrder {
  id: string;
  customer_name: string;
  customer_email: string;
  customer_phone: string;
  shipping_address: string;
  shipping_city: string;
  shipping_notes: string;
  subtotal: number;
  shipping_cost: number;
  total: number;
  status: "pending" | "paid" | "fulfilled" | "delivered" | "cancelled";
  notes: string;
  access_token: string;
  paid_at: string | null;
  fulfilled_at: string | null;
  delivered_at: string | null;
  cancelled_at: string | null;
  created_at: string;
  updated_at: string;
  lines: OrderLine[];
  payment_proof_url?: string;
  payment_proof_filename?: string;
  payment_proof_uploaded_at?: string;
  payment_method_id?: string;
  payment_reference?: string;
  rider_id?: string;
  rider_fee?: number;
  rider_assigned_at?: string;
  rider_delivered_at?: string;
  rider_paid_at?: string;
  is_urgent: boolean;
  urgent_fee?: number;
}

interface Rider {
  id: string;
  name: string;
  status: "available" | "unavailable";
  active: boolean;
}

const { isMobile } = useMobileMode();
const route = useRoute();
const orderId = route.params.id as string;
const authStore = useAuthStore();
const deliveryEnabled = computed(
  () => authStore.tenant?.deliveryEnabled === true,
);

const order = ref<ShopOrder | null>(null);
const loading = ref(false);
const loadError = ref("");
const actionLoading = ref<string | null>(null);
const actionError = ref("");
const showConfirmCancel = ref(false);
const paymentMethods = ref<PaymentMethod[]>([]);
const riders = ref<Rider[]>([]);
const selectedRiderId = ref("");
const deliveryActionLoading = ref<string | null>(null);
const deliveryActionError = ref("");

const riderOptions = computed(() =>
  riders.value
    .filter((r) => r.active && r.status === "available")
    .map((r) => ({ value: r.id, label: r.name })),
);

const assignedRider = computed(() =>
  riders.value.find((r) => r.id === order.value?.rider_id),
);

const deliveryEligible = computed(
  () =>
    order.value !== null &&
    order.value.status !== "cancelled" &&
    order.value.status !== "delivered",
);

function proofIsImage(url?: string, filename?: string): boolean {
  const src = filename ?? url ?? "";
  return /\.(jpe?g|png|webp)$/i.test(src);
}

function proofMethodLabel(): string {
  if (!order.value?.payment_method_id) return "";
  const pm = paymentMethods.value.find(
    (m) => m.id === order.value!.payment_method_id,
  );
  return pm?.label || pm?.type || "";
}

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
    month: "long",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const STATUS_CONFIG: Record<
  string,
  { label: string; class: string; icon: Component }
> = {
  pending: {
    label: "Pendiente de pago",
    class: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
    icon: Clock,
  },
  paid: {
    label: "Pagada",
    class: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
    icon: CreditCard,
  },
  fulfilled: {
    label: "Despachada",
    class:
      "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400",
    icon: Package,
  },
  delivered: {
    label: "Entregada",
    class:
      "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400",
    icon: CheckCircle2,
  },
  cancelled: {
    label: "Cancelada",
    class: "bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400",
    icon: XCircle,
  },
};

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const [orderData, pmData, riderData] = await Promise.allSettled([
      useApiFetch<ShopOrder>(`/api/v1/shop-orders/${orderId}`),
      useApiFetch<PaymentMethod[]>(`/api/v1/payment-methods`),
      deliveryEnabled.value
        ? useApiFetch<Rider[]>(`/api/v1/riders`)
        : Promise.resolve<Rider[]>([]),
    ]);
    if (orderData.status === "fulfilled") {
      order.value = orderData.value;
    } else {
      loadError.value = "No se pudo cargar el pedido";
    }
    if (pmData.status === "fulfilled") {
      paymentMethods.value = pmData.value;
    }
    if (riderData.status === "fulfilled") {
      riders.value = riderData.value;
    }
  } finally {
    loading.value = false;
  }
}

async function performDeliveryAction(
  action:
    | "assign-rider"
    | "auto-assign-rider"
    | "mark-delivered"
    | "mark-rider-paid",
) {
  deliveryActionLoading.value = action;
  deliveryActionError.value = "";
  try {
    const opts: Record<string, unknown> = { method: "POST" };
    if (action === "assign-rider") {
      opts.body = { rider_id: selectedRiderId.value };
    }
    order.value = await useApiFetch<ShopOrder>(
      `/api/v1/orders/${orderId}/${action}`,
      opts,
    );
    selectedRiderId.value = "";
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    deliveryActionError.value =
      e?.data?.detail ?? "No pudimos completar la acción de entrega";
  } finally {
    deliveryActionLoading.value = null;
  }
}

async function toggleUrgent() {
  if (!order.value) return;
  deliveryActionLoading.value = "urgent";
  deliveryActionError.value = "";
  try {
    order.value = await useApiFetch<ShopOrder>(
      `/api/v1/orders/${orderId}/urgent`,
      { method: "PATCH", body: { urgent: !order.value.is_urgent } },
    );
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    deliveryActionError.value =
      e?.data?.detail ?? "No pudimos actualizar la urgencia del pedido";
  } finally {
    deliveryActionLoading.value = null;
  }
}

async function performAction(
  action: "mark-paid" | "mark-fulfilled" | "mark-delivered" | "cancel",
) {
  actionLoading.value = action;
  actionError.value = "";
  showConfirmCancel.value = false;
  try {
    order.value = await useApiFetch<ShopOrder>(
      `/api/v1/shop-orders/${orderId}/${action}`,
      { method: "POST" },
    );
  } catch (err: unknown) {
    const e = err as { data?: { message?: string } };
    actionError.value = e?.data?.message ?? "No pudimos completar la acción";
  } finally {
    actionLoading.value = null;
  }
}

onMounted(load);
</script>

<template>
  <MobileScreensShopOrderDetail
    v-if="isMobile"
    :order="order"
    :loading="loading"
    :load-error="loadError"
    :action-loading="actionLoading"
    :action-error="actionError"
    :payment-methods="paymentMethods"
    @mark-paid="performAction('mark-paid')"
    @mark-fulfilled="performAction('mark-fulfilled')"
    @mark-delivered="performAction('mark-delivered')"
    @cancel="performAction('cancel')"
    @retry="load"
  />
  <div v-else class="p-4 sm:p-6 space-y-5">
    <!-- Back -->
    <div class="flex items-center gap-3">
      <NuxtLink
        to="/shop-orders"
        class="p-2 rounded-lg hover:bg-muted transition-colors text-muted-foreground"
        aria-label="Volver a pedidos online"
      >
        <ArrowLeft class="w-4 h-4" />
      </NuxtLink>
      <div>
        <h1 class="text-xl font-bold text-foreground flex items-center gap-2">
          <ShoppingCart class="w-5 h-5 text-primary" />
          Pedido online
        </h1>
        <p v-if="order" class="text-xs text-muted-foreground font-mono">
          #{{ order.id.slice(0, 8).toUpperCase() }} ·
          {{ fmtDate(order.created_at) }}
        </p>
      </div>
    </div>

    <!-- Loading -->
    <div
      v-if="loading"
      class="flex items-center justify-center py-20 text-muted-foreground"
    >
      <Loader2 class="w-6 h-6 animate-spin mr-2" />
      Cargando...
    </div>

    <!-- Error -->
    <div
      v-else-if="loadError"
      class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3"
    >
      {{ loadError }}
    </div>

    <template v-else-if="order">
      <!-- Status + total header -->
      <div class="border bg-card rounded-xl p-6 space-y-5">
        <div class="flex items-start justify-between gap-4 flex-wrap">
          <div>
            <div class="flex items-center gap-3 mb-2">
              <span
                :class="[
                  'inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm font-medium',
                  STATUS_CONFIG[order.status]?.class,
                ]"
              >
                <component
                  :is="STATUS_CONFIG[order.status]?.icon"
                  class="w-4 h-4"
                />
                {{ STATUS_CONFIG[order.status]?.label }}
              </span>
            </div>
            <p class="text-lg font-semibold text-foreground">
              {{ order.customer_name }}
            </p>
            <p
              class="text-sm text-muted-foreground flex items-center gap-2 mt-0.5"
            >
              <Mail class="w-3.5 h-3.5" />
              <a
                :href="`mailto:${order.customer_email}`"
                class="hover:text-primary hover:underline"
              >
                {{ order.customer_email }}
              </a>
              <span
                v-if="order.customer_phone"
                class="ml-2 flex items-center gap-1"
              >
                <Phone class="w-3.5 h-3.5" />
                {{ order.customer_phone }}
              </span>
            </p>
          </div>
          <div class="text-right">
            <p class="text-3xl font-bold text-foreground font-mono">
              Bs.S {{ fmtCurrency(order.total) }}
            </p>
            <p class="text-xs text-muted-foreground mt-0.5">Total del pedido</p>
          </div>
        </div>

        <!-- Timeline -->
        <div
          class="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs pt-3 border-t"
        >
          <div>
            <p class="text-muted-foreground">Recibido</p>
            <p class="text-foreground font-medium mt-0.5">
              {{ fmtDate(order.created_at) }}
            </p>
          </div>
          <div>
            <p class="text-muted-foreground">Pagado</p>
            <p class="text-foreground font-medium mt-0.5">
              {{ fmtDate(order.paid_at) }}
            </p>
          </div>
          <div>
            <p class="text-muted-foreground">Despachado</p>
            <p class="text-foreground font-medium mt-0.5">
              {{ fmtDate(order.fulfilled_at) }}
            </p>
          </div>
          <div>
            <p class="text-muted-foreground">Entregado</p>
            <p class="text-foreground font-medium mt-0.5">
              {{ fmtDate(order.delivered_at) }}
            </p>
          </div>
        </div>

        <!-- Payment proof section -->
        <div
          v-if="order.payment_proof_url || order.status === 'pending'"
          class="border rounded-xl p-5 space-y-3"
          :class="
            order.payment_proof_url
              ? 'border-emerald-200 bg-emerald-50/60'
              : 'border-border bg-muted/20'
          "
        >
          <h3
            class="text-sm font-bold flex items-center gap-2"
            :class="
              order.payment_proof_url ? 'text-emerald-800' : 'text-foreground'
            "
          >
            <Paperclip class="w-4 h-4" />
            Comprobante de pago
          </h3>

          <!-- Proof present -->
          <template v-if="order.payment_proof_url">
            <!-- Image preview -->
            <a
              v-if="
                proofIsImage(
                  order.payment_proof_url,
                  order.payment_proof_filename,
                )
              "
              :href="order.payment_proof_url"
              target="_blank"
              rel="noopener noreferrer"
              class="block"
            >
              <img
                :src="order.payment_proof_url"
                :alt="order.payment_proof_filename ?? 'Comprobante'"
                class="max-h-96 rounded-lg border object-contain w-full cursor-zoom-in"
              />
            </a>

            <!-- PDF link -->
            <a
              v-else
              :href="order.payment_proof_url"
              target="_blank"
              rel="noopener noreferrer"
              download
              class="inline-flex items-center gap-2 text-sm text-primary hover:underline"
            >
              <FileText class="w-4 h-4" />
              {{ order.payment_proof_filename ?? "Ver comprobante" }}
              <Download class="w-3.5 h-3.5" />
            </a>

            <dl class="text-xs text-muted-foreground space-y-1 mt-2">
              <div v-if="order.payment_proof_filename" class="flex gap-2">
                <dt class="w-24 shrink-0">Archivo</dt>
                <dd class="font-mono">{{ order.payment_proof_filename }}</dd>
              </div>
              <div v-if="order.payment_proof_uploaded_at" class="flex gap-2">
                <dt class="w-24 shrink-0">Subido</dt>
                <dd>{{ fmtDate(order.payment_proof_uploaded_at) }}</dd>
              </div>
              <div v-if="proofMethodLabel()" class="flex gap-2">
                <dt class="w-24 shrink-0">Método</dt>
                <dd>{{ proofMethodLabel() }}</dd>
              </div>
              <div v-if="order.payment_reference" class="flex gap-2">
                <dt class="w-24 shrink-0">Referencia</dt>
                <dd class="font-mono">{{ order.payment_reference }}</dd>
              </div>
            </dl>
          </template>

          <!-- No proof yet, order pending -->
          <p v-else class="text-xs text-muted-foreground">
            El cliente aún no subió el comprobante.
          </p>
        </div>

        <!-- Delivery (motorizados) -->
        <div v-if="deliveryEnabled" class="border rounded-xl p-5 space-y-3">
          <h3 class="text-sm font-bold text-foreground flex items-center gap-2">
            <Bike class="w-4 h-4 text-primary" />
            Entrega a domicilio
          </h3>

          <div
            v-if="deliveryActionError"
            class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3"
          >
            {{ deliveryActionError }}
          </div>

          <!-- No rider assigned yet -->
          <div
            v-if="!order.rider_id && deliveryEligible"
            class="flex items-center gap-2 flex-wrap"
          >
            <Select
              v-model="selectedRiderId"
              :options="riderOptions"
              placeholder="Elegir motorizado..."
              class="max-w-xs"
            />
            <button
              type="button"
              :disabled="!selectedRiderId || deliveryActionLoading !== null"
              class="px-3 py-2 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="performDeliveryAction('assign-rider')"
            >
              <Loader2
                v-if="deliveryActionLoading === 'assign-rider'"
                class="w-4 h-4 animate-spin"
              />
              Asignar
            </button>
            <button
              type="button"
              :disabled="deliveryActionLoading !== null"
              class="px-3 py-2 text-sm rounded-lg border hover:bg-muted transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="performDeliveryAction('auto-assign-rider')"
            >
              <Loader2
                v-if="deliveryActionLoading === 'auto-assign-rider'"
                class="w-4 h-4 animate-spin"
              />
              <Bike v-else class="w-4 h-4" />
              Auto-asignar
            </button>
          </div>

          <!-- Rider assigned -->
          <div v-else-if="order.rider_id" class="space-y-2">
            <p class="text-sm text-foreground">
              Motorizado:
              <span class="font-medium">{{
                assignedRider?.name ?? order.rider_id
              }}</span>
              <span
                v-if="order.rider_fee != null"
                class="text-xs text-muted-foreground ml-2"
              >
                (tarifa Bs.S {{ fmtCurrency(order.rider_fee) }})
              </span>
            </p>

            <p
              v-if="order.rider_delivered_at"
              class="text-xs text-emerald-600 flex items-center gap-1"
            >
              <CheckCircle2 class="w-3.5 h-3.5" />
              Entregado el {{ fmtDate(order.rider_delivered_at) }}
            </p>
            <button
              v-else-if="deliveryEligible"
              type="button"
              :disabled="deliveryActionLoading !== null"
              class="px-3 py-2 text-sm rounded-lg bg-emerald-600 text-white hover:bg-emerald-700 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="performDeliveryAction('mark-delivered')"
            >
              <Loader2
                v-if="deliveryActionLoading === 'mark-delivered'"
                class="w-4 h-4 animate-spin"
              />
              <CheckCircle2 v-else class="w-4 h-4" />
              Marcar entregado por el motorizado
            </button>

            <p
              v-if="order.rider_paid_at"
              class="text-xs text-muted-foreground flex items-center gap-1"
            >
              <DollarSign class="w-3.5 h-3.5" />
              Pagado al motorizado el {{ fmtDate(order.rider_paid_at) }}
            </p>
            <button
              v-else-if="order.rider_delivered_at && order.rider_fee != null"
              type="button"
              :disabled="deliveryActionLoading !== null"
              class="px-3 py-2 text-sm rounded-lg border hover:bg-muted transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="performDeliveryAction('mark-rider-paid')"
            >
              <Loader2
                v-if="deliveryActionLoading === 'mark-rider-paid'"
                class="w-4 h-4 animate-spin"
              />
              <DollarSign v-else class="w-4 h-4" />
              Marcar motorizado como pagado
            </button>
          </div>

          <!-- Urgent toggle -->
          <div
            v-if="deliveryEligible"
            class="flex items-center gap-3 pt-2 border-t"
          >
            <Switch
              :model-value="order.is_urgent"
              :disabled="deliveryActionLoading !== null"
              @update:model-value="toggleUrgent"
            />
            <span class="text-sm text-muted-foreground flex items-center gap-1">
              <Zap class="w-3.5 h-3.5" />
              Pedido urgente
              <span v-if="order.urgent_fee != null" class="text-xs">
                (recargo Bs.S {{ fmtCurrency(order.urgent_fee) }})
              </span>
            </span>
          </div>
        </div>

        <!-- Action error -->
        <div
          v-if="actionError"
          class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3 flex items-center gap-2"
        >
          <AlertTriangle class="w-4 h-4 shrink-0" />
          {{ actionError }}
        </div>

        <!-- Action buttons -->
        <div
          v-if="order.status !== 'delivered' && order.status !== 'cancelled'"
          class="flex items-center gap-3 flex-wrap"
        >
          <!-- pending → paid -->
          <button
            v-if="order.status === 'pending'"
            type="button"
            :disabled="actionLoading !== null"
            class="px-4 py-2 text-sm rounded-lg bg-blue-600 text-white hover:bg-blue-700 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
            @click="performAction('mark-paid')"
          >
            <Loader2
              v-if="actionLoading === 'mark-paid'"
              class="w-4 h-4 animate-spin"
            />
            <CreditCard v-else class="w-4 h-4" />
            Confirmar pago
          </button>

          <!-- paid → fulfilled -->
          <button
            v-if="order.status === 'paid'"
            type="button"
            :disabled="actionLoading !== null"
            class="px-4 py-2 text-sm rounded-lg bg-purple-600 text-white hover:bg-purple-700 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
            @click="performAction('mark-fulfilled')"
          >
            <Loader2
              v-if="actionLoading === 'mark-fulfilled'"
              class="w-4 h-4 animate-spin"
            />
            <Send v-else class="w-4 h-4" />
            Marcar como despachada
          </button>

          <!-- fulfilled → delivered -->
          <button
            v-if="order.status === 'fulfilled'"
            type="button"
            :disabled="actionLoading !== null"
            class="px-4 py-2 text-sm rounded-lg bg-emerald-600 text-white hover:bg-emerald-700 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
            @click="performAction('mark-delivered')"
          >
            <Loader2
              v-if="actionLoading === 'mark-delivered'"
              class="w-4 h-4 animate-spin"
            />
            <CheckCircle2 v-else class="w-4 h-4" />
            Marcar como entregada
          </button>

          <!-- Cancel (pending or paid) -->
          <div v-if="!showConfirmCancel">
            <button
              v-if="order.status === 'pending' || order.status === 'paid'"
              type="button"
              :disabled="actionLoading !== null"
              class="px-4 py-2 text-sm rounded-lg border border-destructive/50 text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="showConfirmCancel = true"
            >
              <XCircle class="w-4 h-4" />
              Cancelar pedido
            </button>
          </div>
          <div v-else class="flex items-center gap-2">
            <span class="text-sm text-muted-foreground">¿Estás seguro?</span>
            <button
              type="button"
              :disabled="actionLoading !== null"
              class="px-3 py-1.5 text-sm rounded-lg bg-destructive text-destructive-foreground hover:bg-destructive/90 transition-colors disabled:opacity-50 cursor-pointer flex items-center gap-2"
              @click="performAction('cancel')"
            >
              <Loader2
                v-if="actionLoading === 'cancel'"
                class="w-3.5 h-3.5 animate-spin"
              />
              Sí, cancelar
            </button>
            <button
              type="button"
              class="px-3 py-1.5 text-sm rounded-lg border hover:bg-muted transition-colors cursor-pointer"
              @click="showConfirmCancel = false"
            >
              No
            </button>
          </div>
        </div>

        <!-- Terminal state info -->
        <div
          v-if="order.status === 'delivered'"
          class="flex items-center gap-2 text-sm text-emerald-600"
        >
          <CheckCircle2 class="w-4 h-4" />
          Pedido entregado el {{ fmtDate(order.delivered_at) }}
        </div>
        <div
          v-if="order.status === 'cancelled'"
          class="flex items-center gap-2 text-sm text-rose-600"
        >
          <XCircle class="w-4 h-4" />
          Pedido cancelado el {{ fmtDate(order.cancelled_at) }}
        </div>
      </div>

      <!-- Two columns: lines + customer/shipping -->
      <div class="grid grid-cols-1 lg:grid-cols-3 gap-5">
        <!-- Lines (2 cols) -->
        <div class="border bg-card rounded-xl overflow-hidden lg:col-span-2">
          <div class="px-6 py-4 border-b border-border">
            <h3 class="font-semibold text-foreground">
              Líneas del pedido ({{ order.lines?.length ?? 0 }})
            </h3>
          </div>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b border-border bg-muted/30">
                  <th
                    class="px-4 py-3 text-left font-medium text-muted-foreground"
                  >
                    Producto
                  </th>
                  <th
                    class="px-4 py-3 text-right font-medium text-muted-foreground"
                  >
                    Cant.
                  </th>
                  <th
                    class="px-4 py-3 text-right font-medium text-muted-foreground hidden md:table-cell"
                  >
                    Precio
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
                  v-for="line in order.lines"
                  :key="line.id"
                  class="hover:bg-muted/20 transition-colors"
                >
                  <td class="px-4 py-3 text-foreground">
                    {{ line.name }}
                    <span
                      v-if="line.is_fiscal"
                      class="ml-1 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold bg-primary/10 text-primary"
                    >
                      FISCAL
                    </span>
                  </td>
                  <td
                    class="px-4 py-3 text-right font-mono text-muted-foreground"
                  >
                    {{ line.quantity }}
                  </td>
                  <td
                    class="px-4 py-3 text-right font-mono text-muted-foreground hidden md:table-cell"
                  >
                    Bs.S {{ fmtCurrency(line.unit_price) }}
                  </td>
                  <td
                    class="px-4 py-3 text-right font-mono font-medium text-foreground"
                  >
                    Bs.S {{ fmtCurrency(line.subtotal) }}
                  </td>
                </tr>
              </tbody>
              <tfoot>
                <tr class="border-t border-border bg-muted/20">
                  <td
                    colspan="3"
                    class="px-4 py-2 text-right text-xs text-muted-foreground"
                  >
                    Subtotal
                  </td>
                  <td class="px-4 py-2 text-right font-mono text-foreground">
                    Bs.S {{ fmtCurrency(order.subtotal) }}
                  </td>
                </tr>
                <tr class="bg-muted/20">
                  <td
                    colspan="3"
                    class="px-4 py-2 text-right text-xs text-muted-foreground"
                  >
                    Envío
                  </td>
                  <td
                    :class="[
                      'px-4 py-2 text-right font-mono',
                      order.shipping_cost === 0
                        ? 'text-emerald-600'
                        : 'text-foreground',
                    ]"
                  >
                    {{
                      order.shipping_cost === 0
                        ? "Gratis"
                        : `Bs.S ${fmtCurrency(order.shipping_cost)}`
                    }}
                  </td>
                </tr>
                <tr class="border-t-2 border-border bg-muted/30">
                  <td
                    colspan="3"
                    class="px-4 py-3 text-right text-sm font-semibold text-foreground"
                  >
                    Total
                  </td>
                  <td
                    class="px-4 py-3 text-right font-mono font-bold text-lg text-foreground"
                  >
                    Bs.S {{ fmtCurrency(order.total) }}
                  </td>
                </tr>
              </tfoot>
            </table>
          </div>
        </div>

        <!-- Customer/Shipping sidebar -->
        <div class="space-y-4">
          <div class="border bg-card rounded-xl p-5">
            <h3
              class="text-sm font-bold text-foreground flex items-center gap-2 mb-3"
            >
              <Truck class="w-4 h-4 text-primary" />
              Envío
            </h3>
            <p class="text-sm text-foreground">{{ order.shipping_address }}</p>
            <p
              v-if="order.shipping_city"
              class="text-xs text-muted-foreground mt-1"
            >
              {{ order.shipping_city }}
            </p>
            <p
              v-if="order.shipping_notes"
              class="text-xs text-muted-foreground italic mt-2 pt-2 border-t"
            >
              "{{ order.shipping_notes }}"
            </p>
          </div>

          <div
            v-if="order.notes"
            class="border bg-amber-50 border-amber-200 rounded-xl p-5"
          >
            <h3 class="text-sm font-bold text-amber-900 mb-2">
              Notas del cliente
            </h3>
            <p class="text-sm text-amber-800">{{ order.notes }}</p>
          </div>

          <!-- Customer tracking link -->
          <div class="border bg-card rounded-xl p-5">
            <h3 class="text-sm font-bold text-foreground mb-2">
              Link de tracking del cliente
            </h3>
            <p class="text-xs text-muted-foreground mb-3">
              Compartí este link con tu cliente si lo necesita:
            </p>
            <a
              :href="`/t/${$route.params.tenantSlug ?? ''}/order/${order.id}?access_token=${order.access_token}`"
              target="_blank"
              class="text-xs text-primary hover:underline break-all flex items-center gap-1"
            >
              <ExternalLink class="w-3 h-3 shrink-0" />
              Ver como cliente
            </a>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
