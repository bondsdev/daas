<script setup lang="ts">
import { useApiFetch, useAuth } from "~/composables/useAuth";
import type { RiderProfile } from "~/components/mobile/screens/Perfil.vue";

// Rider-only surface: no owner/employee chrome, same pattern as my-deliveries.vue.
definePageMeta({ layout: false, middleware: "auth" });

interface HistoryResponse {
  earnings_this_week: number;
  deliveries_this_week: number;
  pending_payout: number;
}

const { logout } = useAuth();

const profile = ref<RiderProfile | null>(null);
const earningsThisWeek = ref(0);
const deliveriesThisWeek = ref(0);
const pendingPayout = ref(0);
const loading = ref(false);
const loadError = ref("");
const statusError = ref("");
const togglingStatus = ref(false);

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const [p, hist] = await Promise.all([
      useApiFetch<RiderProfile>("/api/v1/my-profile"),
      useApiFetch<HistoryResponse>("/api/v1/my-deliveries/history"),
    ]);
    profile.value = p;
    earningsThisWeek.value = hist.earnings_this_week;
    deliveriesThisWeek.value = hist.deliveries_this_week;
    pendingPayout.value = hist.pending_payout;
  } catch {
    loadError.value = "No se pudo cargar tu perfil.";
  } finally {
    loading.value = false;
  }
}

async function toggleStatus() {
  if (!profile.value) return;
  const previous = profile.value.status;
  const next = previous === "available" ? "unavailable" : "available";
  statusError.value = "";
  togglingStatus.value = true;
  profile.value.status = next;
  try {
    await useApiFetch("/api/v1/my-profile/status", {
      method: "PATCH",
      body: { status: next },
    });
  } catch {
    profile.value.status = previous;
    statusError.value = "No se pudo actualizar tu disponibilidad.";
  } finally {
    togglingStatus.value = false;
  }
}

async function handleLogout() {
  await logout();
  await navigateTo("/auth/login");
}

onMounted(load);
</script>

<template>
  <div class="h-screen overflow-hidden bg-background">
    <MobileScreensPerfil
      :profile="profile"
      :earnings-this-week="earningsThisWeek"
      :deliveries-this-week="deliveriesThisWeek"
      :pending-payout="pendingPayout"
      :loading="loading"
      :load-error="loadError"
      :status-error="statusError"
      :toggling-status="togglingStatus"
      @toggle-status="toggleStatus"
      @logout="handleLogout"
    />
    <MobileRiderBottomNav />
  </div>
</template>
