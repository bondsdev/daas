<script setup lang="ts">
import {
  MapPin,
  Zap,
  AlertTriangle,
  Loader2,
  CheckCircle2,
  Truck,
  Navigation,
  MessageCircle,
  Map as MapIcon,
  ChevronRight,
} from "lucide-vue-next";

export interface MyDelivery {
  id: string;
  customer_name: string;
  customer_phone: string;
  shipping_address: string;
  shipping_city: string;
  delivery_lat: number | null;
  delivery_lng: number | null;
  total: number;
  rider_fee: number | null;
  is_urgent: boolean;
}

const props = defineProps<{
  deliveries: MyDelivery[];
  loading: boolean;
  loadError: string;
  markingId: string | null;
}>();

const emit = defineEmits<{
  markDelivered: [orderId: string];
  retry: [];
}>();

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

function locationUrl(order: MyDelivery) {
  if (order.delivery_lat != null && order.delivery_lng != null) {
    return `https://www.google.com/maps/search/?api=1&query=${order.delivery_lat},${order.delivery_lng}`;
  }
  const query = encodeURIComponent(
    [order.shipping_address, order.shipping_city].filter(Boolean).join(", "),
  );
  return `https://www.google.com/maps/search/?api=1&query=${query}`;
}

function whatsappUrl(order: MyDelivery) {
  const digits = order.customer_phone.replace(/\D/g, "");
  return `https://wa.me/${digits}`;
}

// The route-map screen only makes sense when at least one assigned
// delivery has a pin — otherwise there's nothing to map.
const hasMappableDeliveries = computed(() =>
  props.deliveries.some(
    (d) => d.delivery_lat != null && d.delivery_lng != null,
  ),
);
</script>

<template>
  <MobileScreen
    title="Mis entregas"
    subtitle="Pedidos asignados"
    variant="brand"
  >
    <div v-if="loadError" class="px-6 py-20 text-center">
      <AlertTriangle class="mx-auto mb-4 size-12 text-destructive/30" />
      <p class="font-medium text-destructive">{{ loadError }}</p>
      <button
        type="button"
        class="no-min-tap mt-4 h-10 rounded-xl border border-input px-5 text-sm font-semibold text-muted-foreground active:bg-accent"
        @click="emit('retry')"
      >
        Reintentar
      </button>
    </div>

    <div
      v-else-if="loading && !deliveries.length"
      class="flex justify-center py-16"
    >
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
    </div>

    <div v-else-if="!deliveries.length" class="px-6 py-20 text-center">
      <Truck class="mx-auto mb-4 size-12 text-muted-foreground/20" />
      <p class="font-medium text-muted-foreground">
        No tenés entregas asignadas
      </p>
    </div>

    <div v-else class="bg-white">
      <NuxtLink
        v-if="hasMappableDeliveries"
        to="/my-deliveries/ruta"
        class="no-min-tap mx-4 mt-3 flex items-center gap-3 rounded-2xl bg-primary px-4 py-3.5 text-white"
      >
        <span
          class="flex size-10 shrink-0 items-center justify-center rounded-xl bg-white/15"
        >
          <MapIcon class="size-5" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-[13.5px] font-bold leading-tight">
            Ver ruta de hoy
          </span>
          <span class="block text-[11.5px] text-white/75">
            Un solo mapa con todas tus paradas
          </span>
        </span>
        <ChevronRight class="size-4 shrink-0 text-white/70" />
      </NuxtLink>

      <div class="divide-y divide-border border-t border-border">
        <MobileListItem
          v-for="order in deliveries"
          :key="order.id"
          :chevron="false"
          :accent="order.is_urgent ? 'danger' : 'none'"
        >
          <template #leading>
            <div
              class="flex size-11 items-center justify-center rounded-2xl"
              :class="order.is_urgent ? 'bg-red-50' : 'bg-primary/10'"
            >
              <component
                :is="order.is_urgent ? Zap : MapPin"
                class="size-5"
                :class="order.is_urgent ? 'text-red-500' : 'text-primary'"
              />
            </div>
          </template>

          <p
            class="truncate text-sm font-semibold leading-tight text-foreground"
          >
            {{ order.customer_name }}
          </p>
          <p class="mt-0.5 truncate text-xs text-muted-foreground">
            {{ order.shipping_address }}
            <span v-if="order.shipping_city">, {{ order.shipping_city }}</span>
          </p>

          <div class="mt-1.5 flex items-center gap-2">
            <span
              v-if="order.is_urgent"
              class="rounded px-1.5 py-px text-[10px] font-bold bg-red-100 text-red-600"
            >
              URGENTE
            </span>
            <span
              class="font-mono text-xs font-bold tabular-nums text-foreground"
            >
              {{ fmtCurrency(order.total) }}
            </span>
            <span
              v-if="order.rider_fee != null"
              class="text-[11px] text-muted-foreground"
            >
              · tu pago: {{ fmtCurrency(order.rider_fee) }}
            </span>
          </div>

          <div class="mt-2 flex gap-1.5">
            <a
              :href="locationUrl(order)"
              target="_blank"
              rel="noopener"
              class="no-min-tap flex h-9 flex-1 items-center justify-center gap-1.5 rounded-lg border border-input text-xs font-semibold text-foreground"
            >
              <Navigation class="size-3.5" />
              {{
                order.delivery_lat != null ? "Ver ubicación" : "Ver dirección"
              }}
            </a>
            <a
              :href="whatsappUrl(order)"
              target="_blank"
              rel="noopener"
              class="no-min-tap flex h-9 flex-1 items-center justify-center gap-1.5 rounded-lg border border-input text-xs font-semibold text-emerald-600"
            >
              <MessageCircle class="size-3.5" />
              WhatsApp
            </a>
          </div>

          <button
            type="button"
            :disabled="markingId === order.id"
            class="no-min-tap mt-2 flex h-9 w-full items-center justify-center gap-1.5 rounded-lg bg-primary text-sm font-semibold text-white transition-opacity disabled:opacity-60"
            @click="emit('markDelivered', order.id)"
          >
            <Loader2
              v-if="markingId === order.id"
              class="size-4 animate-spin"
            />
            <CheckCircle2 v-else class="size-4" />
            {{ markingId === order.id ? "Marcando…" : "Marcar entregado" }}
          </button>
        </MobileListItem>
      </div>
    </div>
  </MobileScreen>
</template>
