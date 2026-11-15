<script setup lang="ts">
import { useApiFetch } from "~/composables/useAuth";
import type { MyDelivery } from "~/components/mobile/screens/MyDeliveries.vue";

// Rider-only surface: always mobile-first, no owner/employee chrome.
// `layout: false` keeps the panel's sidebar/topbar/BottomNav off this page —
// a rider never has a desktop equivalent to fall back to.
definePageMeta({ layout: false, middleware: "auth" });

const deliveries = ref<MyDelivery[]>([]);
const loading = ref(false);
const loadError = ref("");
const markingId = ref<string | null>(null);

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    deliveries.value = await useApiFetch<MyDelivery[]>("/api/v1/my-deliveries");
  } catch {
    loadError.value = "No se pudieron cargar tus entregas.";
  } finally {
    loading.value = false;
  }
}

async function markDelivered(orderId: string) {
  markingId.value = orderId;
  try {
    await useApiFetch(`/api/v1/orders/${orderId}/mark-delivered`, {
      method: "POST",
    });
    await load();
  } catch {
    loadError.value = "No se pudo marcar la entrega. Intentá de nuevo.";
  } finally {
    markingId.value = null;
  }
}

onMounted(load);
</script>

<template>
  <div class="h-screen overflow-hidden bg-background">
    <MobileScreensMyDeliveries
      :deliveries="deliveries"
      :loading="loading"
      :load-error="loadError"
      :marking-id="markingId"
      @mark-delivered="markDelivered"
    />
  </div>
</template>
