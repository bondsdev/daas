<script setup lang="ts">
import {
  MapPin,
  Loader2,
  AlertCircle,
  CheckCircle2,
  RotateCcw,
} from "lucide-vue-next";
import "leaflet/dist/leaflet.css";

export interface RouteStop {
  order_id: string;
  customer_name: string;
  address: string;
  rider_fee: number | null;
  is_urgent: boolean;
  lat: number;
  lng: number;
}

// Coordinates are [lng, lat] pairs — GeoJSON/ORS convention, the opposite
// of this app's usual lat/lng order — straight from the backend's ORS
// response, so L.geoJSON can consume them with zero extra decoding.
export interface RouteGeometry {
  coordinates: [number, number][];
  distance_meters: number;
  duration_seconds: number;
}

const props = defineProps<{
  riderPosition: { lat: number; lng: number } | null;
  geoError: string;
  stops: RouteStop[];
  route: RouteGeometry | null;
  routeLoading: boolean;
  routeError: string;
  markingId: string | null;
  isAvailable: boolean;
  earningsThisWeek: number;
}>();

const emit = defineEmits<{
  retryLocation: [];
  markDelivered: [orderId: string];
}>();

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

// The CTA always targets the first/urgent stop in route order — same
// ordering ListMyDeliveries already returns, so its position is always 1.
// Computed (rather than a literal "1") to stay honest if that assumption
// ever changes.
const nextStop = computed(() => props.stops[0] ?? null);
const nextStopPosition = computed(() =>
  nextStop.value ? props.stops.indexOf(nextStop.value) + 1 : 1,
);

// ── Leaflet map ───────────────────────────────────────────────────────────
// Mirrors DeliveryLocationPicker.vue's Leaflet-in-Vue init technique (lazy
// `import("leaflet")`, inline-styled divIcon markers), but driven by props
// instead of v-model: the rider position and route arrive asynchronously
// after a one-shot navigator.geolocation call + API fetch owned by the page,
// so the map is built once the first position lands and re-rendered
// whenever stops/route change.
const mapEl = ref<HTMLDivElement | null>(null);
let map: import("leaflet").Map | null = null;
let layerGroup: import("leaflet").LayerGroup | null = null;
let mapInited = false;

function meIcon(L: typeof import("leaflet")) {
  return L.divIcon({
    className: "",
    html: '<div style="width:28px;height:28px;border-radius:50%;background:var(--primary);border:3px solid var(--background);box-shadow:0 2px 6px rgba(0,0,0,.25);"></div>',
    iconSize: [28, 28],
    iconAnchor: [14, 14],
  });
}

function stopIcon(L: typeof import("leaflet"), label: string, urgent: boolean) {
  // Urgent stops use the destructive token, non-urgent stops the
  // foreground token — both real CSS variables from this project's design
  // system (assets/css/main.css), not invented hex values.
  const bg = urgent ? "var(--destructive)" : "var(--foreground)";
  return L.divIcon({
    className: "",
    html: `<div style="width:28px;height:28px;border-radius:50%;background:${bg};border:3px solid var(--background);box-shadow:0 2px 6px rgba(0,0,0,.25);display:flex;align-items:center;justify-content:center;color:#fff;font-weight:800;font-size:12px;">${label}</div>`,
    iconSize: [28, 28],
    iconAnchor: [14, 14],
  });
}

async function renderMarkers() {
  if (!map || !layerGroup || !props.riderPosition) return;
  const L = await import("leaflet");
  layerGroup.clearLayers();

  const bounds: [number, number][] = [
    [props.riderPosition.lat, props.riderPosition.lng],
  ];
  L.marker([props.riderPosition.lat, props.riderPosition.lng], {
    icon: meIcon(L),
  }).addTo(layerGroup);

  props.stops.forEach((stop, i) => {
    L.marker([stop.lat, stop.lng], {
      icon: stopIcon(L, String(i + 1), stop.is_urgent),
    }).addTo(layerGroup!);
    bounds.push([stop.lat, stop.lng]);
  });

  if (props.route && props.route.coordinates.length > 1) {
    const routeFeature: GeoJSON.Feature<GeoJSON.LineString> = {
      type: "Feature",
      properties: {},
      geometry: { type: "LineString", coordinates: props.route.coordinates },
    };
    L.geoJSON(routeFeature, {
      style: { color: "var(--primary)", weight: 4, opacity: 0.85 },
    }).addTo(layerGroup);
  }

  if (bounds.length > 1) {
    map.fitBounds(bounds, { padding: [36, 36] });
  }
}

watch(
  () => props.riderPosition,
  async (pos) => {
    if (!pos || mapInited) return;
    await nextTick();
    if (!mapEl.value) return;
    const L = await import("leaflet");
    map = L.map(mapEl.value).setView([pos.lat, pos.lng], 14);
    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
      attribution: "&copy; OpenStreetMap contributors",
      maxZoom: 19,
    }).addTo(map);
    layerGroup = L.layerGroup().addTo(map);
    mapInited = true;
    await renderMarkers();
  },
  { immediate: true },
);

watch([() => props.stops, () => props.route], () => {
  if (mapInited) void renderMarkers();
});

onBeforeUnmount(() => {
  map?.remove();
  map = null;
  layerGroup = null;
});
</script>

<template>
  <div class="relative h-full bg-background">
    <!-- Geolocation denied/unavailable — do not silently fall back to a
         fixed city coordinate: the whole point here is the rider's real
         position. -->
    <div
      v-if="geoError"
      class="flex h-full flex-col items-center justify-center gap-4 px-8 text-center"
    >
      <MapPin class="size-12 text-muted-foreground/30" />
      <p class="text-sm font-medium text-foreground">{{ geoError }}</p>
      <button
        type="button"
        class="no-min-tap flex h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-white"
        @click="emit('retryLocation')"
      >
        <RotateCcw class="size-4" />
        Reintentar
      </button>
    </div>

    <!-- Position requested, not resolved yet -->
    <div
      v-else-if="!riderPosition"
      class="flex h-full items-center justify-center"
    >
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
    </div>

    <template v-else>
      <div ref="mapEl" class="absolute inset-x-0 top-0" style="height: 58%" />

      <!-- Floating pills -->
      <div
        class="absolute inset-x-4 flex items-center justify-between"
        style="top: calc(0.875rem + env(safe-area-inset-top))"
      >
        <span
          class="flex items-center gap-1.5 rounded-full bg-white px-3 py-1.5 text-xs font-bold shadow-[0_2px_8px_rgba(0,0,0,0.12)]"
        >
          <span
            class="size-[7px] rounded-full"
            :class="isAvailable ? 'bg-emerald-500' : 'bg-muted-foreground'"
          />
          {{ isAvailable ? "En línea" : "No disponible" }}
        </span>
        <span
          class="rounded-full bg-white px-3 py-1.5 font-mono text-xs font-bold shadow-[0_2px_8px_rgba(0,0,0,0.12)]"
        >
          {{ fmtCurrency(earningsThisWeek) }} esta semana
        </span>
      </div>

      <!-- Bottom sheet. Deliberately overlaps the map by a few points (sheet
           top sits above the map's bottom edge) for the layered look the
           approved mock uses — but Leaflet's own attribution control sits
           inside the map at z-index 1000 by default, so without an explicit
           higher z-index here it paints over this sheet's rounded top. -->
      <section
        class="absolute inset-x-0 bottom-0 z-[1100] flex flex-col rounded-t-[22px] bg-background shadow-[0_-8px_24px_rgba(0,0,0,0.1)]"
        style="top: 54%"
      >
        <div class="mx-auto mt-2.5 h-1 w-9 shrink-0 rounded-full bg-border" />
        <div class="flex items-baseline justify-between px-5 pb-2.5 pt-2">
          <span class="text-[15px] font-bold text-foreground"
            >Tu ruta de hoy</span
          >
          <span class="text-xs text-muted-foreground">
            {{ stops.length }} {{ stops.length === 1 ? "parada" : "paradas" }}
          </span>
        </div>

        <div
          v-if="routeError"
          class="mx-4 mb-2 flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-3.5 py-2.5 text-xs text-destructive"
        >
          <AlertCircle class="mt-px size-3.5 shrink-0" />
          <span>
            {{ routeError }} Podés usar "Ver ubicación" en cada pedido desde
            Entregas mientras tanto.
          </span>
        </div>

        <div
          v-if="routeLoading"
          class="flex flex-1 items-center justify-center py-10"
        >
          <Loader2 class="size-5 animate-spin text-muted-foreground" />
        </div>

        <div
          v-else-if="!stops.length"
          class="flex flex-1 flex-col items-center justify-center gap-2 px-8 py-10 text-center"
        >
          <MapPin class="size-9 text-muted-foreground/25" />
          <p class="text-sm font-medium text-muted-foreground">
            No tenés paradas con ubicación para trazar una ruta.
          </p>
        </div>

        <template v-else>
          <div class="flex-1 overflow-y-auto px-4 pb-3">
            <div class="flex flex-col gap-2.5">
              <article
                v-for="(stop, i) in stops"
                :key="stop.order_id"
                class="flex items-start gap-3 rounded-xl border p-3"
                :class="
                  i === 0
                    ? 'border-primary bg-accent'
                    : 'border-border bg-white'
                "
              >
                <span
                  class="mt-0.5 flex size-[26px] shrink-0 items-center justify-center rounded-full text-xs font-extrabold text-white"
                  :class="
                    stop.is_urgent
                      ? 'bg-destructive'
                      : i === 0
                        ? 'bg-primary'
                        : 'bg-foreground'
                  "
                >
                  {{ i + 1 }}
                </span>
                <div class="min-w-0 flex-1">
                  <div class="flex items-baseline justify-between gap-2">
                    <span
                      class="truncate text-[13.5px] font-bold text-foreground"
                    >
                      {{ stop.customer_name }}
                    </span>
                    <span
                      v-if="stop.rider_fee != null"
                      class="shrink-0 font-mono text-xs font-bold text-primary"
                    >
                      {{ fmtCurrency(stop.rider_fee) }}
                    </span>
                  </div>
                  <p class="mt-0.5 text-xs leading-snug text-muted-foreground">
                    {{ stop.address }}
                  </p>
                  <span
                    v-if="stop.is_urgent && i === 0"
                    class="mt-1 inline-block rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] font-extrabold text-destructive"
                  >
                    URGENTE · próxima parada
                  </span>
                  <span
                    v-else-if="stop.is_urgent"
                    class="mt-1 inline-block rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] font-extrabold text-destructive"
                  >
                    URGENTE
                  </span>
                </div>
              </article>
            </div>
          </div>

          <div
            class="border-t border-border px-5 pt-3"
            style="
              padding-bottom: calc(1rem + env(safe-area-inset-bottom) + 4.5rem);
            "
          >
            <button
              type="button"
              :disabled="!nextStop || markingId === nextStop.order_id"
              class="no-min-tap flex h-[50px] w-full items-center justify-center gap-2 rounded-2xl bg-cta text-[15px] font-bold text-white transition-opacity disabled:opacity-60"
              @click="nextStop && emit('markDelivered', nextStop.order_id)"
            >
              <Loader2
                v-if="nextStop && markingId === nextStop.order_id"
                class="size-4 animate-spin"
              />
              <CheckCircle2 v-else class="size-4" />
              Marcar entregado — parada {{ nextStopPosition }}
            </button>
          </div>
        </template>
      </section>
    </template>
  </div>
</template>
