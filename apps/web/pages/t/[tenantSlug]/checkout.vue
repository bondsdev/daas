<script setup lang="ts">
import {
  ArrowLeft,
  ShoppingCart,
  User,
  Mail,
  Phone,
  MapPin,
  ShieldCheck,
  Loader2,
  AlertTriangle,
  Lock,
  Truck,
} from "lucide-vue-next";
import { useCartStore } from "~/stores/cart";
import { useFormatPrice } from "~/composables/useFormatPrice";
import { usePublicFetch } from "~/composables/usePublicFetch";

definePageMeta({
  layout: "public",
});

const route = useRoute();
const router = useRouter();
const tenantSlug = route.params.tenantSlug as string;
const cartStore = useCartStore();
const { format: fmt } = useFormatPrice("VE");

useSeoMeta({
  title: "Checkout · DaaS",
  description: "Finalizá tu compra",
});

// Wait for cart to hydrate from localStorage on the client before deciding
// whether to redirect to the empty cart.
const hydrated = ref(false);
onMounted(() => {
  cartStore.restore();
  hydrated.value = true;
});

watch([hydrated, () => cartStore.isEmpty], ([isHydrated, isEmpty]) => {
  if (isHydrated && isEmpty && !submitting.value && !checkoutSucceeded.value) {
    router.push(`/t/${tenantSlug}/cart`);
  }
});

// ─── Form state ──────────────────────────────────────────────────────────────
const form = reactive({
  customer_name: "",
  customer_email: "",
  customer_phone: "",
  shipping_address: "",
  shipping_city: "",
  shipping_notes: "",
  notes: "",
});

const deliveryLocation = ref<{ lat: number; lng: number } | null>(null);
const fieldErrors = reactive<Record<string, string>>({});
const serverError = ref("");
const submitting = ref(false);
// Set once the checkout succeeds — prevents the cart-empty watcher from
// hijacking the post-success redirect to /order.
const checkoutSucceeded = ref(false);

function validate(): boolean {
  Object.keys(fieldErrors).forEach((k) => delete fieldErrors[k]);
  let ok = true;
  if (!form.customer_name.trim()) {
    fieldErrors.customer_name = "El nombre es obligatorio";
    ok = false;
  }
  if (!form.customer_email.trim()) {
    fieldErrors.customer_email = "El email es obligatorio";
    ok = false;
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.customer_email)) {
    fieldErrors.customer_email = "Ingresá un email válido";
    ok = false;
  }
  if (!form.shipping_address.trim()) {
    fieldErrors.shipping_address = "La dirección es obligatoria";
    ok = false;
  }
  if (!form.customer_phone.trim()) {
    fieldErrors.customer_phone = "El teléfono es obligatorio";
    ok = false;
  }
  if (!deliveryLocation.value) {
    fieldErrors.delivery_location = "Marcá el punto de entrega en el mapa";
    ok = false;
  }
  return ok;
}

// ─── Mocked summary totals (mirror cart page rules) ──────────────────────────
const shippingCost = computed(() => (cartStore.subtotal > 100 ? 0 : 5));
const total = computed(() => cartStore.subtotal + shippingCost.value);

// ─── Submit ──────────────────────────────────────────────────────────────────
interface CheckoutResponse {
  id: string;
  access_token: string;
  status: string;
  total: number;
}

async function submitCheckout() {
  serverError.value = "";
  if (!validate()) return;

  submitting.value = true;
  try {
    const payload = {
      customer_name: form.customer_name.trim(),
      customer_email: form.customer_email.trim(),
      customer_phone: form.customer_phone.trim(),
      shipping_address: form.shipping_address.trim(),
      shipping_city: form.shipping_city.trim(),
      shipping_notes: form.shipping_notes.trim(),
      notes: form.notes.trim(),
      delivery_lat: deliveryLocation.value!.lat,
      delivery_lng: deliveryLocation.value!.lng,
      lines: cartStore.items.map((i) => ({
        product_id: i.productId,
        quantity: i.qty,
      })),
    };
    const order = await usePublicFetch<CheckoutResponse>("/checkout", {
      method: "POST",
      body: payload,
    });
    // Mark success BEFORE clearing the cart so the empty-cart watcher
    // doesn't fire its own redirect during the same tick.
    checkoutSucceeded.value = true;
    cartStore.clearCart();
    await router.push(
      `/t/${tenantSlug}/order/${order.id}?access_token=${order.access_token}&new=1`,
    );
  } catch (err: unknown) {
    const e = err as {
      data?: { error?: string; message?: string; field?: string };
    };
    const errCode = e?.data?.error ?? "";
    if (errCode === "insufficient_stock") {
      serverError.value =
        "Uno o más productos no tienen stock suficiente. Revisá tu carrito.";
    } else if (errCode === "product_not_purchasable") {
      serverError.value =
        "Uno o más productos ya no están disponibles. Revisá tu carrito.";
    } else {
      serverError.value =
        e?.data?.message ?? "No pudimos procesar tu pedido. Probá de nuevo.";
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="bg-background min-h-[60vh]">
    <!-- Page header -->
    <div class="border-b bg-card/50">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 py-5 flex items-center gap-3">
        <NuxtLink
          :to="`/t/${tenantSlug}/cart`"
          class="p-2 rounded-lg hover:bg-muted text-muted-foreground transition-colors"
          aria-label="Volver al carrito"
        >
          <ArrowLeft class="w-4 h-4" />
        </NuxtLink>
        <div>
          <h1
            class="text-xl sm:text-2xl font-bold font-heading text-foreground flex items-center gap-2"
          >
            <Lock class="w-5 h-5 text-primary" />
            Checkout seguro
          </h1>
          <p class="text-xs text-muted-foreground">
            Completá tus datos para finalizar la compra
          </p>
        </div>
      </div>
    </div>

    <!-- Hydration / empty guard -->
    <div
      v-if="!hydrated"
      class="max-w-7xl mx-auto px-4 sm:px-6 py-20 flex items-center justify-center text-muted-foreground"
    >
      <Loader2 class="w-6 h-6 animate-spin mr-2" />
      Cargando...
    </div>

    <!-- Layout -->
    <form
      v-else
      class="max-w-7xl mx-auto px-4 sm:px-6 py-6 grid grid-cols-1 lg:grid-cols-[1fr_380px] gap-6 lg:gap-8 pb-32 lg:pb-8"
      @submit.prevent="submitCheckout"
    >
      <!-- Form -->
      <section class="space-y-5">
        <!-- Server error -->
        <div
          v-if="serverError"
          class="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 flex items-start gap-3 text-sm text-destructive"
        >
          <AlertTriangle class="w-5 h-5 shrink-0 mt-0.5" />
          <div class="flex-1">
            <p class="font-semibold">No pudimos procesar el pedido</p>
            <p>{{ serverError }}</p>
          </div>
        </div>

        <!-- Datos de contacto -->
        <div class="border bg-card rounded-xl p-5 shadow-sm">
          <h2
            class="text-base font-bold text-foreground mb-1 flex items-center gap-2"
          >
            <User class="w-4 h-4 text-primary" />
            Datos de contacto
          </h2>
          <p class="text-xs text-muted-foreground mb-4">
            Usaremos esto para confirmarte el pedido
          </p>

          <div class="space-y-4">
            <div>
              <label
                for="customer_name"
                class="block text-xs font-semibold text-foreground mb-1.5"
              >
                Nombre completo <span class="text-destructive">*</span>
              </label>
              <input
                id="customer_name"
                v-model="form.customer_name"
                type="text"
                autocomplete="name"
                :class="[
                  'w-full h-11 px-3 rounded-lg border bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/20',
                  fieldErrors.customer_name
                    ? 'border-destructive focus:border-destructive'
                    : 'border-input focus:border-primary',
                ]"
                placeholder="Juan Pérez"
              />
              <p
                v-if="fieldErrors.customer_name"
                class="text-xs text-destructive mt-1"
              >
                {{ fieldErrors.customer_name }}
              </p>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label
                  for="customer_email"
                  class="block text-xs font-semibold text-foreground mb-1.5"
                >
                  <Mail class="w-3 h-3 inline mr-1" />
                  Email <span class="text-destructive">*</span>
                </label>
                <input
                  id="customer_email"
                  v-model="form.customer_email"
                  type="email"
                  autocomplete="email"
                  :class="[
                    'w-full h-11 px-3 rounded-lg border bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/20',
                    fieldErrors.customer_email
                      ? 'border-destructive focus:border-destructive'
                      : 'border-input focus:border-primary',
                  ]"
                  placeholder="juan@email.com"
                />
                <p
                  v-if="fieldErrors.customer_email"
                  class="text-xs text-destructive mt-1"
                >
                  {{ fieldErrors.customer_email }}
                </p>
              </div>
              <div>
                <label
                  for="customer_phone"
                  class="block text-xs font-semibold text-foreground mb-1.5"
                >
                  <Phone class="w-3 h-3 inline mr-1" />
                  Teléfono <span class="text-destructive">*</span>
                </label>
                <input
                  id="customer_phone"
                  v-model="form.customer_phone"
                  type="tel"
                  autocomplete="tel"
                  :class="[
                    'w-full h-11 px-3 rounded-lg border bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/20',
                    fieldErrors.customer_phone
                      ? 'border-destructive focus:border-destructive'
                      : 'border-input focus:border-primary',
                  ]"
                  placeholder="+58 412 555 1234"
                />
                <p
                  v-if="fieldErrors.customer_phone"
                  class="text-xs text-destructive mt-1"
                >
                  {{ fieldErrors.customer_phone }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <!-- Envío -->
        <div class="border bg-card rounded-xl p-5 shadow-sm">
          <h2
            class="text-base font-bold text-foreground mb-1 flex items-center gap-2"
          >
            <Truck class="w-4 h-4 text-primary" />
            Dirección de envío
          </h2>
          <p class="text-xs text-muted-foreground mb-4">
            Asegurate que sea correcta para no demorar la entrega
          </p>

          <div class="space-y-4">
            <div>
              <label
                for="shipping_address"
                class="block text-xs font-semibold text-foreground mb-1.5"
              >
                <MapPin class="w-3 h-3 inline mr-1" />
                Dirección completa <span class="text-destructive">*</span>
              </label>
              <input
                id="shipping_address"
                v-model="form.shipping_address"
                type="text"
                autocomplete="street-address"
                :class="[
                  'w-full h-11 px-3 rounded-lg border bg-white text-sm focus:outline-none focus:ring-2 focus:ring-primary/20',
                  fieldErrors.shipping_address
                    ? 'border-destructive focus:border-destructive'
                    : 'border-input focus:border-primary',
                ]"
                placeholder="Av. Principal, Edif. Las Palmas, Apto 4B"
              />
              <p
                v-if="fieldErrors.shipping_address"
                class="text-xs text-destructive mt-1"
              >
                {{ fieldErrors.shipping_address }}
              </p>
            </div>

            <div>
              <label class="block text-xs font-semibold text-foreground mb-1.5">
                <MapPin class="w-3 h-3 inline mr-1" />
                Punto exacto de entrega <span class="text-destructive">*</span>
              </label>
              <StorefrontDeliveryLocationPicker v-model="deliveryLocation" />
              <p
                v-if="fieldErrors.delivery_location"
                class="text-xs text-destructive mt-1"
              >
                {{ fieldErrors.delivery_location }}
              </p>
            </div>

            <div>
              <label
                for="shipping_city"
                class="block text-xs font-semibold text-foreground mb-1.5"
              >
                Ciudad
              </label>
              <input
                id="shipping_city"
                v-model="form.shipping_city"
                type="text"
                autocomplete="address-level2"
                class="w-full h-11 px-3 rounded-lg border border-input bg-white text-sm focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
                placeholder="Caracas"
              />
            </div>

            <div>
              <label
                for="shipping_notes"
                class="block text-xs font-semibold text-foreground mb-1.5"
              >
                Indicaciones adicionales
              </label>
              <textarea
                id="shipping_notes"
                v-model="form.shipping_notes"
                rows="2"
                class="w-full px-3 py-2 rounded-lg border border-input bg-white text-sm focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20 resize-none"
                placeholder="Casa azul al lado del kiosco, llamar al llegar..."
              />
            </div>
          </div>
        </div>

        <!-- Notas -->
        <div class="border bg-card rounded-xl p-5 shadow-sm">
          <h2 class="text-base font-bold text-foreground mb-1">
            Notas para el vendedor
          </h2>
          <p class="text-xs text-muted-foreground mb-4">Opcional</p>
          <textarea
            v-model="form.notes"
            rows="2"
            class="w-full px-3 py-2 rounded-lg border border-input bg-white text-sm focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20 resize-none"
            placeholder="¿Algo que el vendedor deba saber sobre tu pedido?"
          />
        </div>

        <!-- Submit row (desktop) -->
        <div class="hidden lg:flex items-center justify-end pt-2">
          <button
            type="submit"
            :disabled="submitting || !deliveryLocation"
            class="px-6 py-3.5 rounded-xl bg-primary text-white text-base font-bold flex items-center gap-2 hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed transition-opacity cursor-pointer shadow-lg shadow-primary/20"
          >
            <Loader2 v-if="submitting" class="w-5 h-5 animate-spin" />
            <Lock v-else class="w-5 h-5" />
            {{ submitting ? "Procesando..." : "Confirmar pedido" }}
          </button>
        </div>
      </section>

      <!-- Order summary (sticky) -->
      <aside>
        <div class="lg:sticky lg:top-20 space-y-4">
          <div class="border bg-card rounded-xl p-5 shadow-sm">
            <h2 class="text-base font-bold text-foreground mb-4">Tu pedido</h2>

            <!-- Line items -->
            <ul class="space-y-3 mb-4 max-h-72 overflow-y-auto pr-1">
              <li
                v-for="item in cartStore.items"
                :key="item.productId"
                class="flex items-start gap-3 pb-3 border-b last:border-b-0 last:pb-0"
              >
                <div
                  class="w-10 h-10 rounded-md bg-primary/10 flex items-center justify-center shrink-0 text-primary text-xs font-bold"
                >
                  {{ item.qty }}
                </div>
                <div class="flex-1 min-w-0">
                  <p
                    class="text-sm font-medium text-foreground line-clamp-2 leading-tight"
                  >
                    {{ item.name }}
                  </p>
                  <p
                    v-if="item.is_fiscal"
                    class="text-[10px] text-muted-foreground"
                  >
                    Fiscal · IVA incluido
                  </p>
                </div>
                <p
                  class="text-sm font-semibold font-mono text-foreground whitespace-nowrap"
                >
                  {{ fmt(item.price * item.qty) }}
                </p>
              </li>
            </ul>

            <!-- Totals -->
            <dl class="space-y-2 text-sm pt-3 border-t">
              <div class="flex items-center justify-between">
                <dt class="text-muted-foreground">Subtotal</dt>
                <dd class="font-semibold text-foreground font-mono">
                  {{ fmt(cartStore.subtotal) }}
                </dd>
              </div>
              <div class="flex items-center justify-between">
                <dt class="text-muted-foreground">Envío</dt>
                <dd
                  :class="[
                    'font-semibold font-mono',
                    shippingCost === 0 ? 'text-emerald-600' : 'text-foreground',
                  ]"
                >
                  {{ shippingCost === 0 ? "Gratis" : fmt(shippingCost) }}
                </dd>
              </div>
              <div class="h-px bg-border my-2" />
              <div class="flex items-center justify-between text-base">
                <dt class="font-bold text-foreground">Total</dt>
                <dd class="font-bold text-foreground font-mono text-xl">
                  {{ fmt(total) }}
                </dd>
              </div>
            </dl>

            <!-- Submit row (desktop, inside summary) -->
            <button
              type="submit"
              :disabled="submitting || !deliveryLocation"
              class="hidden lg:flex w-full mt-5 px-5 py-3.5 rounded-xl bg-primary text-white text-sm font-bold items-center justify-center gap-2 hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer shadow-lg shadow-primary/20"
              @click.prevent="submitCheckout"
            >
              <Loader2 v-if="submitting" class="w-4 h-4 animate-spin" />
              <Lock v-else class="w-4 h-4" />
              {{ submitting ? "Procesando..." : "Confirmar pedido" }}
            </button>

            <p
              class="text-[11px] text-center text-muted-foreground mt-2.5 flex items-center justify-center gap-1"
            >
              <ShieldCheck class="w-3 h-3" />
              Pago seguro · Confirmación por email
            </p>
          </div>

          <!-- Trust footer -->
          <div
            class="border bg-card rounded-xl p-4 space-y-2 text-xs text-muted-foreground"
          >
            <div class="flex items-center gap-2">
              <ShieldCheck class="w-4 h-4 text-primary shrink-0" />
              Tus datos están protegidos
            </div>
            <div class="flex items-center gap-2">
              <Truck class="w-4 h-4 text-primary shrink-0" />
              Envío en 1-3 días hábiles
            </div>
          </div>
        </div>
      </aside>

      <!-- Sticky mobile submit bar -->
      <div
        class="fixed bottom-0 inset-x-0 lg:hidden bg-card/95 backdrop-blur border-t shadow-lg z-30 px-4 pt-3"
        style="padding-bottom: calc(0.75rem + env(safe-area-inset-bottom))"
      >
        <button
          type="submit"
          :disabled="submitting || !deliveryLocation"
          class="flex h-14 w-full items-center justify-between rounded-2xl bg-primary px-5 text-primary-foreground shadow-md shadow-primary/25 disabled:opacity-50 disabled:cursor-not-allowed"
          @click.prevent="submitCheckout"
        >
          <div class="flex items-center gap-2 text-sm font-bold">
            <Loader2 v-if="submitting" class="size-4 animate-spin" />
            <Lock v-else class="size-4" />
            {{ submitting ? "Procesando..." : "Confirmar pedido" }}
          </div>
          <span class="font-mono font-bold">{{ fmt(total) }}</span>
        </button>
      </div>
    </form>
  </div>
</template>
