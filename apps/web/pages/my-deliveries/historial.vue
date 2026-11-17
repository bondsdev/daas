<script setup lang="ts">
import { useApiFetch } from "~/composables/useAuth";
import type { HistoryDelivery } from "~/components/mobile/screens/Historial.vue";

// Rider-only surface: no owner/employee chrome, same pattern as my-deliveries.vue.
definePageMeta({ layout: false, middleware: "auth" });

interface HistoryResponse {
  deliveries: HistoryDelivery[];
  earnings_this_week: number;
  deliveries_this_week: number;
  pending_payout: number;
}

const deliveries = ref<HistoryDelivery[]>([]);
const earningsThisWeek = ref(0);
const deliveriesThisWeek = ref(0);
const pendingPayout = ref(0);
const loading = ref(false);
const loadError = ref("");

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await useApiFetch<HistoryResponse>(
      "/api/v1/my-deliveries/history",
    );
    deliveries.value = res.deliveries;
    earningsThisWeek.value = res.earnings_this_week;
    deliveriesThisWeek.value = res.deliveries_this_week;
    pendingPayout.value = res.pending_payout;
  } catch {
    loadError.value = "No se pudo cargar tu historial.";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="h-screen overflow-hidden bg-background">
    <MobileScreensHistorial
      :deliveries="deliveries"
      :earnings-this-week="earningsThisWeek"
      :deliveries-this-week="deliveriesThisWeek"
      :pending-payout="pendingPayout"
      :loading="loading"
      :load-error="loadError"
    />
    <MobileRiderBottomNav />
  </div>
</template>
