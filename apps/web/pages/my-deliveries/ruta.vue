<script setup lang="ts">
import { useApiFetch } from "~/composables/useAuth";
import type {
  RouteStop,
  RouteGeometry,
} from "~/components/mobile/screens/MapaRuta.vue";

// Rider-only surface: no owner/employee chrome, same flat-sibling pattern
// as my-deliveries/{historial,perfil}.vue.
definePageMeta({ layout: false, middleware: "auth" });

interface RiderProfileResponse {
  status: "available" | "unavailable";
}

interface HistoryResponse {
  earnings_this_week: number;
}

interface MyRouteResponse {
  stops: RouteStop[];
  route?: RouteGeometry | null;
}

const riderPosition = ref<{ lat: number; lng: number } | null>(null);
const geoError = ref("");

const stops = ref<RouteStop[]>([]);
const route = ref<RouteGeometry | null>(null);
const routeLoading = ref(false);
const routeError = ref("");

const isAvailable = ref(false);
const earningsThisWeek = ref(0);
const markingId = ref<string | null>(null);

// Supporting pills: rider online status + this-week earnings. There is no
// "today" earnings figure anywhere in the existing API — only
// earnings_this_week from /my-deliveries/history — so the pill is labeled
// "esta semana" rather than faking a daily number.
async function loadProfileAndEarnings() {
  try {
    const [profile, hist] = await Promise.all([
      useApiFetch<RiderProfileResponse>("/api/v1/my-profile"),
      useApiFetch<HistoryResponse>("/api/v1/my-deliveries/history"),
    ]);
    isAvailable.value = profile.status === "available";
    earningsThisWeek.value = hist.earnings_this_week;
  } catch {
    // Non-critical for this screen — pills just show their defaults.
  }
}

async function loadRoute() {
  if (!riderPosition.value) return;
  routeLoading.value = true;
  routeError.value = "";
  try {
    const res = await useApiFetch<MyRouteResponse>(
      `/api/v1/my-deliveries/route?lat=${riderPosition.value.lat}&lng=${riderPosition.value.lng}`,
    );
    stops.value = res.stops;
    route.value = res.route ?? null;
  } catch {
    routeError.value = "No se pudo calcular la ruta en este momento.";
  } finally {
    routeLoading.value = false;
  }
}

// One-shot position snapshot — not watchPosition/live tracking. Live GPS
// tracking was evaluated and rejected earlier in this project: browsers
// pause GPS in the background, which would need native Capacitor plus a
// paid plugin.
function requestLocation() {
  geoError.value = "";
  if (!("geolocation" in navigator)) {
    geoError.value =
      "Tu navegador no soporta geolocalización — no podemos trazar tu ruta.";
    return;
  }
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      riderPosition.value = {
        lat: pos.coords.latitude,
        lng: pos.coords.longitude,
      };
      void loadRoute();
    },
    () => {
      geoError.value =
        "No pudimos obtener tu ubicación — activá el GPS e intentá de nuevo.";
    },
    { enableHighAccuracy: true, timeout: 10000 },
  );
}

async function markDelivered(orderId: string) {
  markingId.value = orderId;
  try {
    await useApiFetch(`/api/v1/orders/${orderId}/mark-delivered`, {
      method: "POST",
    });
    await loadRoute();
  } catch {
    routeError.value = "No se pudo marcar la entrega. Intentá de nuevo.";
  } finally {
    markingId.value = null;
  }
}

onMounted(() => {
  requestLocation();
  void loadProfileAndEarnings();
});
</script>

<template>
  <div class="h-screen overflow-hidden bg-background">
    <MobileScreensMapaRuta
      :rider-position="riderPosition"
      :geo-error="geoError"
      :stops="stops"
      :route="route"
      :route-loading="routeLoading"
      :route-error="routeError"
      :marking-id="markingId"
      :is-available="isAvailable"
      :earnings-this-week="earningsThisWeek"
      @retry-location="requestLocation"
      @mark-delivered="markDelivered"
    />
    <MobileRiderBottomNav />
  </div>
</template>
