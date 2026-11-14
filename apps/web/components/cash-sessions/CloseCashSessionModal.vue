<script setup lang="ts">
import { X, Loader2, AlertTriangle } from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";

interface Props {
  open: boolean;
}

defineProps<Props>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  closed: [sessionId: string];
}>();

const notes = ref("");
const saving = ref(false);
const serverError = ref("");

function close() {
  emit("update:open", false);
  reset();
}

function reset() {
  notes.value = "";
  serverError.value = "";
}

async function confirm() {
  serverError.value = "";
  saving.value = true;
  try {
    const session = await useApiFetch<{ id: string }>(
      "/api/v1/cash-sessions/close",
      { method: "POST", body: { notes: notes.value.trim() } },
    );
    emit("closed", session.id);
    close();
  } catch (err: unknown) {
    const e = err as { data?: { detail?: string; message?: string } };
    serverError.value =
      e?.data?.detail ?? e?.data?.message ?? "Error al cerrar la caja";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
    >
      <div class="absolute inset-0 bg-black/50" @click="close" />
      <div class="relative z-10 w-full max-w-md bg-white rounded-xl shadow-xl">
        <!-- Header -->
        <div
          class="flex items-center justify-between px-6 py-5 border-b border-border"
        >
          <h2 class="text-xl font-bold font-heading text-foreground">
            Cerrar caja
          </h2>
          <button
            type="button"
            class="p-1.5 rounded-lg text-muted-foreground hover:bg-gray-100 transition-all duration-200 cursor-pointer"
            @click="close"
          >
            <X class="w-5 h-5" />
          </button>
        </div>

        <!-- Body -->
        <div class="px-6 py-5 space-y-5">
          <p class="text-sm text-muted-foreground">
            Esta acción cierra la sesión de caja actual. No podrás registrar más
            ventas hasta abrir una nueva.
          </p>

          <div
            v-if="serverError"
            class="rounded-lg bg-red-50 border border-red-200 px-4 py-3 flex items-start gap-2"
          >
            <AlertTriangle class="w-4 h-4 text-red-500 flex-shrink-0 mt-0.5" />
            <p class="text-sm text-red-600">{{ serverError }}</p>
          </div>

          <div>
            <label
              for="close-notes"
              class="block text-sm font-semibold text-foreground mb-1.5"
            >
              Notas de cierre
              <span class="text-muted-foreground font-normal ml-1"
                >(opcional)</span
              >
            </label>
            <textarea
              id="close-notes"
              v-model="notes"
              rows="3"
              placeholder="Ej: Cierre sin novedades..."
              class="w-full px-4 py-3 border border-input rounded-lg text-base transition-all duration-200 resize-none focus:outline-none focus:ring-2 focus:border-primary focus:ring-primary/20 placeholder:text-muted-foreground"
            />
          </div>
        </div>

        <!-- Footer -->
        <div
          class="px-6 py-4 border-t border-border flex items-center justify-end gap-3"
        >
          <button
            type="button"
            class="h-10 px-5 border border-input text-muted-foreground text-sm font-semibold rounded-lg hover:bg-gray-50 transition-all duration-200 cursor-pointer"
            @click="close"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="saving"
            class="flex items-center gap-2 h-10 px-6 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
            @click="confirm"
          >
            <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
            <span>{{ saving ? "Cerrando..." : "Confirmar cierre" }}</span>
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
