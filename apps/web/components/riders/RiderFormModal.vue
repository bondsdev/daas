<script setup lang="ts">
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "~/components/ui/dialog";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Select } from "~/components/ui/select";
import { Switch } from "~/components/ui/switch";
import { Loader2 } from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";

export interface Rider {
  id: string;
  tenant_id: string;
  user_id: string;
  name: string;
  phone: string;
  rate_per_delivery: number;
  status: "available" | "unavailable";
  max_concurrent_deliveries: number;
  last_assigned_at: string | null;
  active: boolean;
  created_at: string;
  updated_at: string;
}

interface UserOption {
  id: string;
  email: string;
  active: boolean;
}

const props = defineProps<{
  open: boolean;
  rider?: Rider | null;
}>();

const emit = defineEmits<{
  close: [];
  saved: [rider: Rider];
}>();

const saving = ref(false);
const saveError = ref("");
const users = ref<UserOption[]>([]);

const form = reactive({
  user_id: "",
  name: "",
  phone: "",
  rate_per_delivery: 0,
  max_concurrent_deliveries: 1,
  status: "available" as Rider["status"],
  active: true,
});

const isEdit = computed(() => !!props.rider);
const title = computed(() =>
  isEdit.value ? "Editar motorizado" : "Nuevo motorizado",
);
const userOptions = computed(() =>
  users.value
    .filter((u) => u.active)
    .map((u) => ({ value: u.id, label: u.email })),
);

watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    saveError.value = "";
    if (!isEdit.value && users.value.length === 0) {
      try {
        users.value = await useApiFetch<UserOption[]>("/api/v1/users");
      } catch {
        // non-fatal — select just stays empty
      }
    }
    if (props.rider) {
      form.user_id = props.rider.user_id;
      form.name = props.rider.name;
      form.phone = props.rider.phone;
      form.rate_per_delivery = props.rider.rate_per_delivery;
      form.max_concurrent_deliveries = props.rider.max_concurrent_deliveries;
      form.status = props.rider.status;
      form.active = props.rider.active;
    } else {
      form.user_id = "";
      form.name = "";
      form.phone = "";
      form.rate_per_delivery = 0;
      form.max_concurrent_deliveries = 1;
      form.status = "available";
      form.active = true;
    }
  },
);

async function save() {
  if (!form.name.trim()) {
    saveError.value = "El nombre es obligatorio";
    return;
  }
  if (!isEdit.value && !form.user_id) {
    saveError.value = "Elegí el usuario que será este motorizado";
    return;
  }
  saving.value = true;
  saveError.value = "";
  try {
    let result: Rider;
    if (isEdit.value && props.rider) {
      result = await useApiFetch<Rider>(`/api/v1/riders/${props.rider.id}`, {
        method: "PATCH",
        body: {
          status: form.status,
          max_concurrent_deliveries: form.max_concurrent_deliveries,
          rate_per_delivery: form.rate_per_delivery,
          active: form.active,
        },
      });
    } else {
      result = await useApiFetch<Rider>("/api/v1/riders", {
        method: "POST",
        body: {
          user_id: form.user_id,
          name: form.name,
          phone: form.phone,
          rate_per_delivery: form.rate_per_delivery,
          max_concurrent_deliveries: form.max_concurrent_deliveries,
        },
      });
    }
    emit("saved", result);
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string } };
    saveError.value =
      e?.data?.detail ?? "Ocurrió un error al guardar el motorizado";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('close')">
    <DialogContent class="max-w-md">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
      </DialogHeader>

      <div class="space-y-4">
        <div
          v-if="saveError"
          class="bg-destructive/10 border border-destructive/30 text-destructive text-sm rounded-lg px-4 py-3"
        >
          {{ saveError }}
        </div>

        <div v-if="!isEdit" class="space-y-1.5">
          <Label for="rider-user">Usuario</Label>
          <Select
            id="rider-user"
            v-model="form.user_id"
            :options="userOptions"
            placeholder="Elegir usuario..."
          />
        </div>

        <div class="space-y-1.5">
          <Label for="rider-name">Nombre</Label>
          <Input
            id="rider-name"
            v-model="form.name"
            :disabled="isEdit"
            placeholder="Pedro Pérez"
          />
        </div>

        <div class="space-y-1.5">
          <Label for="rider-phone">Teléfono</Label>
          <Input
            id="rider-phone"
            v-model="form.phone"
            :disabled="isEdit"
            placeholder="+58 412 000 0000"
          />
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1.5">
            <Label for="rider-rate">Tarifa por entrega</Label>
            <Input
              id="rider-rate"
              v-model="form.rate_per_delivery"
              type="number"
              min="0"
              step="0.01"
            />
          </div>
          <div class="space-y-1.5">
            <Label for="rider-max">Entregas simultáneas</Label>
            <Input
              id="rider-max"
              v-model="form.max_concurrent_deliveries"
              type="number"
              min="1"
              step="1"
            />
          </div>
        </div>

        <div v-if="isEdit" class="space-y-1.5">
          <Label for="rider-status">Disponibilidad</Label>
          <Select
            id="rider-status"
            v-model="form.status"
            :options="[
              { value: 'available', label: 'Disponible' },
              { value: 'unavailable', label: 'No disponible' },
            ]"
          />
        </div>

        <div v-if="isEdit" class="flex items-center gap-3 pt-1">
          <Switch v-model="form.active" />
          <span class="text-sm text-muted-foreground">{{
            form.active ? "Motorizado activo" : "Motorizado inactivo"
          }}</span>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('close')">Cancelar</Button>
        <Button :disabled="saving" @click="save">
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          {{ saving ? "Guardando..." : "Guardar" }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
