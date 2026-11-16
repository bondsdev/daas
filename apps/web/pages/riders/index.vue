<script setup lang="ts">
import { Bike, Plus, Pencil, Loader2, FolderOpen, Lock } from "lucide-vue-next";
import { Badge } from "~/components/ui/badge";
import { Switch } from "~/components/ui/switch";
import { useApiFetch } from "~/composables/useAuth";
import { useAuthStore } from "~/stores/auth";
import RiderFormModal from "~/components/riders/RiderFormModal.vue";
import type { Rider } from "~/components/riders/RiderFormModal.vue";

definePageMeta({
  layout: "default",
  middleware: "auth",
});

const authStore = useAuthStore();
const deliveryEnabled = computed(
  () => authStore.tenant?.deliveryEnabled === true,
);

const riders = ref<Rider[]>([]);
const loading = ref(false);
const loadError = ref("");
const formModalOpen = ref(false);
const editingRider = ref<Rider | null>(null);
const togglingId = ref<string | null>(null);

async function loadRiders() {
  loading.value = true;
  loadError.value = "";
  try {
    riders.value = await useApiFetch<Rider[]>("/api/v1/riders");
  } catch {
    loadError.value = "No se pudieron cargar los motorizados";
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingRider.value = null;
  formModalOpen.value = true;
}

function openEdit(rider: Rider) {
  editingRider.value = rider;
  formModalOpen.value = true;
}

function onSaved(rider: Rider) {
  const idx = riders.value.findIndex((r) => r.id === rider.id);
  if (idx >= 0) {
    riders.value[idx] = rider;
  } else {
    riders.value.unshift(rider);
  }
  formModalOpen.value = false;
}

async function toggleActive(rider: Rider) {
  togglingId.value = rider.id;
  try {
    const updated = await useApiFetch<Rider>(`/api/v1/riders/${rider.id}`, {
      method: "PATCH",
      body: {
        status: rider.status,
        max_concurrent_deliveries: rider.max_concurrent_deliveries,
        rate_per_delivery: rider.rate_per_delivery,
        active: !rider.active,
      },
    });
    const idx = riders.value.findIndex((r) => r.id === updated.id);
    if (idx >= 0) riders.value[idx] = updated;
  } catch {
    // ignore — toggle stays as-is, user can retry
  } finally {
    togglingId.value = null;
  }
}

onMounted(() => {
  if (deliveryEnabled.value) loadRiders();
});
</script>

<template>
  <div class="p-4 sm:p-6 space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">Motorizados</h1>
        <p class="text-sm text-muted-foreground mt-0.5">
          Gestioná a tus riders de entrega a domicilio
        </p>
      </div>
      <button
        v-if="deliveryEnabled"
        class="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
        @click="openCreate"
      >
        <Plus class="w-4 h-4" />
        Nuevo motorizado
      </button>
    </div>

    <!-- Delivery not enabled for this tenant -->
    <div
      v-if="!deliveryEnabled"
      class="flex flex-col items-center justify-center py-20 text-center gap-3 border-2 border-dashed border-border rounded-xl"
    >
      <Lock class="w-12 h-12 opacity-30 text-muted-foreground" />
      <p class="text-sm font-semibold text-foreground">
        La entrega a domicilio no está habilitada para tu cuenta
      </p>
      <p class="text-xs text-muted-foreground max-w-md">
        Esta función se activa por nuestro equipo. Contactá a soporte si querés
        ofrecer entregas con motorizados propios.
      </p>
    </div>

    <template v-else>
      <div
        v-if="loadError"
        class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3"
      >
        {{ loadError }}
      </div>

      <div
        v-if="loading"
        class="flex items-center justify-center py-20 text-muted-foreground"
      >
        <Loader2 class="w-6 h-6 animate-spin mr-2" />
        Cargando motorizados...
      </div>

      <div
        v-else-if="riders.length === 0"
        class="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3 border-2 border-dashed border-border rounded-xl"
      >
        <FolderOpen class="w-12 h-12 opacity-30" />
        <p class="text-sm">Aún no tenés motorizados registrados</p>
        <button
          class="text-sm text-primary hover:underline"
          @click="openCreate"
        >
          Agregar el primero
        </button>
      </div>

      <div v-else class="border bg-card rounded-xl overflow-hidden">
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-border bg-muted/30">
                <th
                  class="px-4 py-3 text-left font-medium text-muted-foreground"
                >
                  Motorizado
                </th>
                <th
                  class="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell"
                >
                  Teléfono
                </th>
                <th
                  class="px-4 py-3 text-right font-medium text-muted-foreground hidden md:table-cell"
                >
                  Tarifa
                </th>
                <th
                  class="px-4 py-3 text-right font-medium text-muted-foreground hidden lg:table-cell"
                >
                  Capacidad
                </th>
                <th
                  class="px-4 py-3 text-left font-medium text-muted-foreground"
                >
                  Estado
                </th>
                <th
                  class="px-4 py-3 text-left font-medium text-muted-foreground"
                >
                  Activo
                </th>
                <th class="px-4 py-3" />
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              <tr
                v-for="rider in riders"
                :key="rider.id"
                class="hover:bg-muted/20 transition-colors group"
              >
                <td class="px-4 py-3">
                  <div class="flex items-center gap-3">
                    <div
                      class="w-9 h-9 rounded-lg bg-primary/10 flex items-center justify-center shrink-0"
                    >
                      <Bike class="w-4 h-4 text-primary" />
                    </div>
                    <p class="font-medium text-foreground">{{ rider.name }}</p>
                  </div>
                </td>
                <td
                  class="px-4 py-3 text-muted-foreground hidden md:table-cell"
                >
                  {{ rider.phone || "—" }}
                </td>
                <td
                  class="px-4 py-3 text-right font-mono text-muted-foreground hidden md:table-cell"
                >
                  {{ rider.rate_per_delivery.toFixed(2) }}
                </td>
                <td
                  class="px-4 py-3 text-right font-mono text-muted-foreground hidden lg:table-cell"
                >
                  {{ rider.max_concurrent_deliveries }}
                </td>
                <td class="px-4 py-3">
                  <Badge
                    :variant="
                      rider.status === 'available' ? 'success' : 'secondary'
                    "
                  >
                    {{
                      rider.status === "available"
                        ? "Disponible"
                        : "No disponible"
                    }}
                  </Badge>
                </td>
                <td class="px-4 py-3">
                  <Switch
                    :model-value="rider.active"
                    :disabled="togglingId === rider.id"
                    @update:model-value="toggleActive(rider)"
                  />
                </td>
                <td class="px-4 py-3">
                  <div
                    class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity justify-end"
                  >
                    <button
                      class="p-1.5 rounded-lg hover:bg-muted transition-colors text-muted-foreground"
                      title="Editar"
                      @click="openEdit(rider)"
                    >
                      <Pencil class="w-4 h-4" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <RiderFormModal
        :open="formModalOpen"
        :rider="editingRider"
        @close="formModalOpen = false"
        @saved="onSaved"
      />
    </template>
  </div>
</template>
