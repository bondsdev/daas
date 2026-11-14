<script setup lang="ts">
import {
  ScanLine,
  Search,
  Plus,
  Minus,
  Trash2,
  ShoppingCart,
  Loader2,
  Package,
  Check,
  AlertTriangle,
  Lock,
  CreditCard,
} from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";
import type { Product } from "~/components/products/ProductFormModal.vue";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

// ─── Types ────────────────────────────────────────────────────────────────────

type PosSearchMode = "barcode" | "text" | "both";
type PaymentMethod = "cash" | "card" | "mobile_payment";

interface CashSession {
  id: string;
  status: "open" | "closed";
}

interface CartLine {
  productId: string;
  name: string;
  unitPrice: number;
  quantity: number;
}

// ─── State ────────────────────────────────────────────────────────────────────

const checkingSession = ref(true);
const sessionOpen = ref(false);
const sessionError = ref("");

const products = ref<Product[]>([]);
const loadingProducts = ref(false);
const searchMode = ref<PosSearchMode>("barcode");

const barcodeInput = ref("");
const barcodeInputEl = ref<HTMLInputElement | null>(null);
const barcodeError = ref("");
const textQuery = ref("");

const cart = ref<CartLine[]>([]);
const paymentMethod = ref<PaymentMethod>("cash");
const currency = "VES";

const charging = ref(false);
const chargeError = ref("");
const lastSaleTotal = ref<number | null>(null);

// ─── Products (fiscal products cannot be sold via POS — internal invoices only) ──

const sellableProducts = computed(() =>
  products.value.filter((p) => p.active && !p.is_fiscal),
);

const textResults = computed(() => {
  const q = textQuery.value.toLowerCase().trim();
  if (!q) return [];
  return sellableProducts.value
    .filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        (p.sku ?? "").toLowerCase().includes(q) ||
        (p.barcode ?? "").toLowerCase().includes(q),
    )
    .slice(0, 20);
});

// ─── Cart ─────────────────────────────────────────────────────────────────────

const cartTotal = computed(() =>
  cart.value.reduce((sum, l) => sum + l.quantity * l.unitPrice, 0),
);

function addToCart(product: Product) {
  const line = cart.value.find((l) => l.productId === product.id);
  if (line) {
    line.quantity++;
  } else {
    cart.value.push({
      productId: product.id,
      name: product.name,
      unitPrice: product.internal_price ?? 0,
      quantity: 1,
    });
  }
}

function increment(idx: number) {
  cart.value[idx].quantity++;
}

function decrement(idx: number) {
  if (cart.value[idx].quantity <= 1) {
    cart.value.splice(idx, 1);
  } else {
    cart.value[idx].quantity--;
  }
}

function removeLine(idx: number) {
  cart.value.splice(idx, 1);
}

function onBarcodeEnter() {
  const code = barcodeInput.value.trim();
  barcodeInput.value = "";
  barcodeError.value = "";
  if (!code) return;
  const product = sellableProducts.value.find((p) => p.barcode === code);
  if (!product) {
    barcodeError.value = `Sin producto para el código "${code}"`;
    return;
  }
  addToCart(product);
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

async function charge() {
  if (cart.value.length === 0) return;
  charging.value = true;
  chargeError.value = "";
  try {
    await useApiFetch("/api/v1/pos/sales", {
      method: "POST",
      body: {
        lines: cart.value.map((l) => ({
          product_id: l.productId,
          quantity: l.quantity,
        })),
        payment_method: paymentMethod.value,
        currency,
      },
    });
    lastSaleTotal.value = cartTotal.value;
    cart.value = [];
  } catch (err: unknown) {
    const e = err as {
      response?: { status?: number };
      status?: number;
      data?: { detail?: string };
    };
    const status = e?.response?.status ?? e?.status;
    if (status === 409) {
      chargeError.value =
        "La caja se cerró durante la venta — abrí una nueva caja para seguir vendiendo.";
      sessionOpen.value = false;
    } else if (status === 422) {
      chargeError.value = e?.data?.detail ?? "Stock insuficiente";
    } else {
      chargeError.value = e?.data?.detail ?? "Error al registrar la venta";
    }
  } finally {
    charging.value = false;
  }
}

function dismissConfirmation() {
  lastSaleTotal.value = null;
}

// ─── Load ─────────────────────────────────────────────────────────────────────

async function checkOpenSession() {
  checkingSession.value = true;
  sessionError.value = "";
  try {
    await useApiFetch<CashSession>("/api/v1/cash-sessions/open");
    sessionOpen.value = true;
  } catch (err: unknown) {
    const e = err as { response?: { status?: number }; status?: number };
    const status = e?.response?.status ?? e?.status;
    if (status === 404) {
      sessionOpen.value = false;
    } else {
      sessionError.value = "Error al consultar el estado de la caja";
    }
  } finally {
    checkingSession.value = false;
  }
}

onMounted(async () => {
  await checkOpenSession();
  if (!sessionOpen.value) return;

  loadingProducts.value = true;
  try {
    const [productData, tenantData] = await Promise.all([
      useApiFetch<Product[]>("/api/v1/products"),
      useApiFetch<{ pos_search_mode?: PosSearchMode }>("/api/v1/tenants/me"),
    ]);
    products.value = productData ?? [];
    searchMode.value = tenantData?.pos_search_mode ?? "barcode";
  } finally {
    loadingProducts.value = false;
  }

  if (searchMode.value !== "text") {
    nextTick(() => barcodeInputEl.value?.focus());
  }
});
</script>

<template>
  <div class="flex flex-col h-full min-h-screen bg-background">
    <!-- Loading session check -->
    <div
      v-if="checkingSession"
      class="flex-1 flex items-center justify-center p-6"
    >
      <Loader2 class="w-6 h-6 animate-spin text-muted-foreground" />
    </div>

    <!-- Error checking session -->
    <div
      v-else-if="sessionError"
      class="flex-1 flex flex-col items-center justify-center gap-3 p-6 text-center"
    >
      <AlertTriangle class="w-10 h-10 text-red-400" />
      <p class="text-red-600 font-medium">{{ sessionError }}</p>
      <button
        type="button"
        class="h-9 px-4 border border-input text-sm font-semibold text-muted-foreground rounded-lg hover:bg-gray-50 cursor-pointer"
        @click="checkOpenSession"
      >
        Reintentar
      </button>
    </div>

    <!-- No open session -->
    <div
      v-else-if="!sessionOpen"
      class="flex-1 flex flex-col items-center justify-center gap-4 p-6 text-center"
    >
      <div
        class="w-14 h-14 rounded-2xl bg-orange-50 flex items-center justify-center"
      >
        <Lock class="w-6 h-6 text-orange-600" />
      </div>
      <div>
        <h1 class="text-lg font-bold font-heading text-foreground">
          Abrí la caja antes de vender
        </h1>
        <p class="text-sm text-muted-foreground mt-1">
          No hay una sesión de caja abierta para este turno
        </p>
      </div>
      <NuxtLink
        to="/cash-sessions"
        class="h-11 px-6 flex items-center gap-2 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200"
      >
        Ir a Caja
      </NuxtLink>
    </div>

    <!-- POS -->
    <div v-else class="flex flex-col flex-1 lg:flex-row overflow-hidden">
      <!-- ── Search / catalog ───────────────────────────────────────────── -->
      <div class="flex-1 flex flex-col overflow-hidden lg:border-r">
        <div class="px-4 sm:px-6 py-4 border-b bg-card space-y-3">
          <!-- Barcode scan input (barcode / both) -->
          <div
            v-if="searchMode === 'barcode' || searchMode === 'both'"
            class="relative"
          >
            <ScanLine
              class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground"
            />
            <input
              ref="barcodeInputEl"
              v-model="barcodeInput"
              type="text"
              class="w-full h-11 pl-9 pr-3 rounded-lg border border-input bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/30 focus:border-primary"
              placeholder="Escaneá el código de barras..."
              @keyup.enter="onBarcodeEnter"
            />
          </div>
          <p v-if="barcodeError" class="text-xs text-red-500">
            {{ barcodeError }}
          </p>

          <!-- Text search (text / both) -->
          <div
            v-if="searchMode === 'text' || searchMode === 'both'"
            class="relative"
          >
            <Search
              class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground"
            />
            <input
              v-model="textQuery"
              type="text"
              class="w-full h-10 pl-9 pr-3 rounded-lg border border-input bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/30 focus:border-primary"
              placeholder="Buscar producto por nombre o SKU..."
            />
          </div>
        </div>

        <div class="flex-1 overflow-y-auto px-4 sm:px-6 py-4">
          <div
            v-if="loadingProducts"
            class="flex items-center justify-center py-20 text-muted-foreground"
          >
            <Loader2 class="w-6 h-6 animate-spin mr-2" />
            Cargando productos...
          </div>

          <!-- Text search results -->
          <div v-else-if="searchMode === 'text' || searchMode === 'both'">
            <div
              v-if="!textQuery"
              class="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3"
            >
              <Package class="w-10 h-10 opacity-30" />
              <p class="text-sm">Escribí para buscar un producto</p>
            </div>
            <div
              v-else-if="textResults.length === 0"
              class="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3"
            >
              <Package class="w-10 h-10 opacity-30" />
              <p class="text-sm">Sin resultados para esa búsqueda</p>
            </div>
            <div v-else class="divide-y divide-border">
              <button
                v-for="product in textResults"
                :key="product.id"
                type="button"
                class="w-full flex items-center justify-between gap-3 py-3 text-left hover:bg-gray-50/80 transition-colors cursor-pointer"
                @click="addToCart(product)"
              >
                <div class="min-w-0">
                  <p class="text-sm font-semibold text-foreground truncate">
                    {{ product.name }}
                  </p>
                  <p
                    v-if="product.sku"
                    class="text-xs text-muted-foreground font-mono"
                  >
                    {{ product.sku }}
                  </p>
                </div>
                <div class="flex items-center gap-3 shrink-0">
                  <span class="text-sm font-semibold text-foreground">
                    ${{ (product.internal_price ?? 0).toFixed(2) }}
                  </span>
                  <Plus class="w-4 h-4 text-primary" />
                </div>
              </button>
            </div>
          </div>

          <!-- Barcode-only mode: just a hint, no browsable list -->
          <div
            v-else
            class="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3"
          >
            <ScanLine class="w-10 h-10 opacity-30" />
            <p class="text-sm">Escaneá un producto para agregarlo al carrito</p>
          </div>
        </div>
      </div>

      <!-- ── Cart + checkout ───────────────────────────────────────────── -->
      <div
        class="w-full lg:max-w-md flex flex-col bg-card border-t lg:border-t-0"
      >
        <div class="px-5 py-4 border-b flex items-center justify-between">
          <div class="flex items-center gap-2">
            <ShoppingCart class="w-4 h-4 text-primary" />
            <span class="text-sm font-bold text-foreground">Carrito</span>
          </div>
          <span
            v-if="cart.length > 0"
            class="text-xs text-muted-foreground font-medium"
          >
            {{ cart.length }} ítem{{ cart.length !== 1 ? "s" : "" }}
          </span>
        </div>

        <div
          class="flex-1 overflow-y-auto flex flex-col max-h-[40vh] lg:max-h-none"
        >
          <div
            v-if="cart.length === 0"
            class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-3 px-6 py-10"
          >
            <ShoppingCart class="w-10 h-10 opacity-20" />
            <p class="text-sm text-center">Agregá productos para empezar</p>
          </div>

          <div v-else class="divide-y divide-border">
            <div
              v-for="(line, idx) in cart"
              :key="line.productId"
              class="px-5 py-3 flex items-center gap-3"
            >
              <div class="flex-1 min-w-0">
                <p class="text-sm font-medium text-foreground truncate">
                  {{ line.name }}
                </p>
                <p class="text-xs text-muted-foreground font-mono mt-0.5">
                  ${{ line.unitPrice.toFixed(2) }} c/u
                </p>
              </div>

              <div class="flex items-center gap-1 shrink-0">
                <button
                  type="button"
                  class="w-7 h-7 rounded-lg border border-input hover:bg-muted flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  @click="decrement(idx)"
                >
                  <Minus class="w-3 h-3" />
                </button>
                <span
                  class="w-8 text-center text-sm font-semibold tabular-nums text-foreground"
                >
                  {{ line.quantity }}
                </span>
                <button
                  type="button"
                  class="w-7 h-7 rounded-lg border border-input hover:bg-muted flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  @click="increment(idx)"
                >
                  <Plus class="w-3 h-3" />
                </button>
              </div>

              <p
                class="text-sm font-semibold font-mono text-foreground w-20 text-right shrink-0"
              >
                ${{ (line.quantity * line.unitPrice).toFixed(2) }}
              </p>

              <button
                type="button"
                class="text-muted-foreground hover:text-red-500 transition-colors p-1 rounded cursor-pointer"
                @click="removeLine(idx)"
              >
                <Trash2 class="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>

        <!-- Payment method -->
        <div class="border-t px-5 py-4 space-y-2">
          <p
            class="text-xs font-semibold text-muted-foreground flex items-center gap-1.5"
          >
            <CreditCard class="w-3.5 h-3.5" />
            Método de pago
          </p>
          <div class="grid grid-cols-3 gap-2">
            <button
              type="button"
              :class="[
                'h-9 rounded-lg text-xs font-semibold border transition-colors cursor-pointer',
                paymentMethod === 'cash'
                  ? 'bg-primary/10 border-primary/30 text-primary'
                  : 'border-input text-muted-foreground hover:bg-gray-50',
              ]"
              @click="paymentMethod = 'cash'"
            >
              Efectivo
            </button>
            <button
              type="button"
              :class="[
                'h-9 rounded-lg text-xs font-semibold border transition-colors cursor-pointer',
                paymentMethod === 'card'
                  ? 'bg-primary/10 border-primary/30 text-primary'
                  : 'border-input text-muted-foreground hover:bg-gray-50',
              ]"
              @click="paymentMethod = 'card'"
            >
              Tarjeta
            </button>
            <button
              type="button"
              :class="[
                'h-9 rounded-lg text-xs font-semibold border transition-colors cursor-pointer',
                paymentMethod === 'mobile_payment'
                  ? 'bg-primary/10 border-primary/30 text-primary'
                  : 'border-input text-muted-foreground hover:bg-gray-50',
              ]"
              @click="paymentMethod = 'mobile_payment'"
            >
              Pago móvil
            </button>
          </div>
        </div>

        <!-- Footer: total + charge -->
        <div class="border-t bg-card px-5 py-4 space-y-3">
          <div
            v-if="chargeError"
            class="bg-red-50 border border-red-200 text-red-700 text-xs rounded-lg px-3 py-2"
          >
            {{ chargeError }}
          </div>

          <div
            v-if="lastSaleTotal !== null"
            class="bg-green-50 border border-green-200 text-green-700 text-sm rounded-lg px-3 py-2 flex items-center justify-between gap-2"
          >
            <span class="flex items-center gap-1.5">
              <Check class="w-4 h-4" />
              Venta registrada por ${{ lastSaleTotal.toFixed(2) }}
            </span>
            <button
              type="button"
              class="text-green-700/70 hover:text-green-700 cursor-pointer"
              @click="dismissConfirmation"
            >
              ✕
            </button>
          </div>

          <div class="flex items-center justify-between">
            <span class="text-sm font-semibold text-muted-foreground"
              >Total</span
            >
            <span class="text-2xl font-bold font-heading text-foreground">
              ${{ cartTotal.toFixed(2) }}
            </span>
          </div>

          <button
            type="button"
            :disabled="charging || cart.length === 0"
            class="w-full flex items-center justify-center gap-2 h-12 rounded-xl bg-primary text-white text-sm font-bold hover:opacity-90 transition-all duration-200 disabled:opacity-40 cursor-pointer"
            @click="charge"
          >
            <Loader2 v-if="charging" class="w-4 h-4 animate-spin" />
            <Check v-else class="w-4 h-4" />
            {{ charging ? "Cobrando..." : "Cobrar" }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
