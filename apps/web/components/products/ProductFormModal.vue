<script setup lang="ts">
import { Loader2, X, ImagePlus, Trash2 } from "lucide-vue-next";
import { useApiFetch } from "~/composables/useAuth";

export interface Product {
  id: string;
  tenant_id: string;
  name: string;
  sku?: string;
  barcode?: string;
  description?: string;
  category_id?: string;
  is_fiscal: boolean;
  fiscal_price?: number;
  internal_price?: number;
  price_usd?: number;
  tax_rate?: number;
  unit?: string;
  active: boolean;
  image_url?: string;
  created_at: string;
  updated_at: string;
}

export interface Category {
  id: string;
  tenant_id: string;
  name: string;
  parent_id?: string;
}

interface Props {
  modelValue: boolean;
  product?: Product;
  categories: Category[];
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  saved: [];
}>();

const isEdit = computed(() => !!props.product);
const title = computed(() =>
  isEdit.value ? "Editar Producto" : "Nuevo Producto",
);

// Form state
const name = ref("");
const sku = ref("");
const barcode = ref("");
const description = ref("");
const categoryId = ref("");
const active = ref(true);
const isFiscal = ref(false);
const fiscalPrice = ref<string>("");
const internalPrice = ref<string>("");
const priceUsd = ref<string>("");
const taxRate = ref<string>("");
const unit = ref<string>("unidad");

// True when the USD price field holds a valid, positive value — in that
// case internal_price is a system-computed value (BCV rate), not editable.
const usdPriceActive = computed(() => {
  // priceUsd is bound to a native <input type="number">, which makes Vue
  // auto-apply the `.number` v-model modifier — the value can be a number,
  // not the string the ref type promises. Coerce before string methods.
  const trimmed = String(priceUsd.value ?? "").trim();
  if (!trimmed) return false;
  const n = parseFloat(trimmed);
  return !isNaN(n) && n > 0;
});

// Error state
const errors = ref<Record<string, string>>({});
const saving = ref(false);
const serverError = ref("");

// Image state
const imageFile = ref<File | null>(null);
const imagePreviewURL = ref<string>("");
const uploadingImage = ref(false);
const existingImageURL = ref<string>("");
const removingImage = ref(false);

const imageInputRef = ref<HTMLInputElement | null>(null);

const categoryOptions = computed(() =>
  props.categories.map((c) => ({ value: c.id, label: c.name })),
);

const taxRateOptions = [
  { value: "0", label: "0% — Exento" },
  { value: "8", label: "8% — Reducido" },
  { value: "16", label: "16% — General (IVA Venezuela)" },
];

function resetForm() {
  if (props.product) {
    name.value = props.product.name;
    sku.value = props.product.sku ?? "";
    barcode.value = props.product.barcode ?? "";
    description.value = props.product.description ?? "";
    categoryId.value = props.product.category_id ?? "";
    active.value = props.product.active;
    isFiscal.value = props.product.is_fiscal;
    fiscalPrice.value = props.product.fiscal_price?.toString() ?? "";
    internalPrice.value = props.product.internal_price?.toString() ?? "";
    priceUsd.value = props.product.price_usd?.toString() ?? "";
    taxRate.value = props.product.tax_rate?.toString() ?? "";
    unit.value = props.product.unit ?? "unidad";
    existingImageURL.value = props.product.image_url ?? "";
  } else {
    name.value = "";
    sku.value = "";
    barcode.value = "";
    description.value = "";
    categoryId.value = "";
    active.value = true;
    isFiscal.value = false;
    fiscalPrice.value = "";
    internalPrice.value = "";
    priceUsd.value = "";
    taxRate.value = "";
    unit.value = "unidad";
    existingImageURL.value = "";
  }
  errors.value = {};
  serverError.value = "";
  imageFile.value = null;
  imagePreviewURL.value = "";
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) resetForm();
  },
);

// Clear price fields when type changes
watch(isFiscal, () => {
  fiscalPrice.value = "";
  internalPrice.value = "";
  priceUsd.value = "";
  taxRate.value = "";
  errors.value = {};
});

function validate(): boolean {
  const errs: Record<string, string> = {};

  if (!name.value.trim()) {
    errs.name = "El nombre es obligatorio";
  }

  if (isFiscal.value) {
    const price = parseFloat(fiscalPrice.value);
    if (!fiscalPrice.value || isNaN(price) || price <= 0) {
      errs.fiscalPrice = "El precio fiscal es obligatorio y debe ser mayor a 0";
    }
    if (!taxRate.value) {
      errs.taxRate = "Selecciona la tasa de IVA";
    }
  } else {
    const usdRaw = String(priceUsd.value ?? "").trim();
    if (usdRaw) {
      const usd = parseFloat(usdRaw);
      if (isNaN(usd) || usd <= 0) {
        errs.priceUsd = "El precio en USD debe ser mayor a 0";
      }
    } else {
      const price = parseFloat(internalPrice.value);
      if (!internalPrice.value || isNaN(price) || price <= 0) {
        errs.internalPrice =
          "El precio interno es obligatorio y debe ser mayor a 0";
      }
    }
  }

  errors.value = errs;
  return Object.keys(errs).length === 0;
}

function onImageFileSelected(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  imageFile.value = file;
  imagePreviewURL.value = URL.createObjectURL(file);
}

function clearImageSelection() {
  imageFile.value = null;
  imagePreviewURL.value = "";
  if (imageInputRef.value) imageInputRef.value.value = "";
}

async function uploadImageForProduct(productId: string) {
  if (!imageFile.value) return;
  uploadingImage.value = true;
  try {
    const fd = new FormData();
    fd.append("image", imageFile.value);
    await useApiFetch(`/api/v1/products/${productId}/image`, {
      method: "POST",
      body: fd,
    });
  } catch {
    // Non-fatal — product saved; image upload failure is silent in this flow.
    // The user can retry from the product card on the list page.
  } finally {
    uploadingImage.value = false;
  }
}

async function handleRemoveImage() {
  if (!isEdit.value || !props.product) return;
  removingImage.value = true;
  try {
    await useApiFetch(`/api/v1/products/${props.product.id}/image`, {
      method: "DELETE",
    });
    existingImageURL.value = "";
  } catch {
    // silently ignore
  } finally {
    removingImage.value = false;
  }
}

async function handleSave() {
  if (!validate()) return;

  saving.value = true;
  serverError.value = "";

  try {
    const body: Record<string, unknown> = {
      name: name.value.trim(),
      is_fiscal: isFiscal.value,
      active: active.value,
    };

    if (sku.value.trim()) body.sku = sku.value.trim();
    if (barcode.value.trim()) body.barcode = barcode.value.trim();
    if (description.value.trim()) body.description = description.value.trim();
    if (categoryId.value) body.category_id = categoryId.value;
    body.unit = unit.value.trim() || "unidad";

    if (isFiscal.value) {
      body.fiscal_price = parseFloat(fiscalPrice.value);
      body.tax_rate = parseFloat(taxRate.value);
    } else {
      const usdRaw = String(priceUsd.value ?? "").trim();
      if (usdRaw) {
        // internal_price is computed server-side from price_usd and the
        // tenant's current BCV rate — omit it so the backend's calculated
        // value is never overridden by a stale/manual number here.
        body.price_usd = parseFloat(usdRaw);
      } else {
        // No USD price: internal_price goes back to being a plain manual
        // value, and price_usd is omitted so the backend clears it.
        body.internal_price = parseFloat(internalPrice.value);
      }
    }

    let savedProductId: string;

    if (isEdit.value && props.product) {
      await useApiFetch(`/api/v1/products/${props.product.id}`, {
        method: "PUT",
        body,
      });
      savedProductId = props.product.id;
    } else {
      const created = await useApiFetch<{ product: Product }>(
        "/api/v1/products",
        {
          method: "POST",
          body,
        },
      );
      savedProductId = created.product.id;
    }

    // Step 2: upload image if one was selected (after we have the product ID).
    if (imageFile.value && savedProductId) {
      await uploadImageForProduct(savedProductId);
    }

    emit("update:modelValue", false);
    emit("saved");
  } catch (err: unknown) {
    const apiError = err as { data?: { detail?: string; message?: string } };
    serverError.value =
      apiError?.data?.detail ??
      apiError?.data?.message ??
      "Error al guardar el producto";
  } finally {
    saving.value = false;
  }
}

function handleClose() {
  emit("update:modelValue", false);
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
    >
      <!-- Backdrop -->
      <div class="absolute inset-0 bg-black/50" @click="handleClose" />

      <!-- Modal content — fixed height with internal scroll -->
      <div
        class="relative z-10 w-full max-w-lg bg-white rounded-xl shadow-xl flex flex-col"
        style="max-height: min(90vh, 680px)"
      >
        <!-- Header — sticky top -->
        <div
          class="flex-shrink-0 flex items-center justify-between px-6 py-5 border-b border-border rounded-t-xl"
        >
          <h2 class="text-xl font-bold font-heading text-foreground">
            {{ title }}
          </h2>
          <button
            type="button"
            class="p-1.5 rounded-lg text-muted-foreground hover:text-muted-foreground hover:bg-gray-100 transition-all duration-200 cursor-pointer"
            @click="handleClose"
          >
            <X class="w-5 h-5" />
          </button>
        </div>

        <!-- Body — scrolls internally -->
        <div class="flex-1 overflow-y-auto px-6 py-5 space-y-5">
          <!-- Server error -->
          <div
            v-if="serverError"
            class="rounded-lg bg-red-50 border border-red-200 px-4 py-3"
          >
            <p class="text-sm text-red-600">{{ serverError }}</p>
          </div>

          <!-- Fiscal toggle — prominent -->
          <div
            class="flex items-center justify-between rounded-xl border-2 px-5 py-4 transition-colors duration-200"
            :class="
              isFiscal
                ? 'border-primary bg-primary/5'
                : 'border-input bg-gray-50'
            "
          >
            <div>
              <p class="text-sm font-bold text-foreground">Tipo de producto</p>
              <p class="text-xs text-muted-foreground mt-0.5">
                {{
                  isFiscal
                    ? "Fiscal — emite comprobante, lleva IVA"
                    : "Interno — precio libre, sin IVA"
                }}
              </p>
            </div>
            <div class="flex items-center gap-3">
              <span
                class="text-sm font-semibold"
                :class="isFiscal ? 'text-muted-foreground' : 'text-foreground'"
                >Interno</span
              >
              <Switch v-model="isFiscal" class="flex-shrink-0" />
              <span
                class="text-sm font-semibold"
                :class="isFiscal ? 'text-primary' : 'text-muted-foreground'"
                >Fiscal</span
              >
            </div>
          </div>

          <!-- Name (required) -->
          <div>
            <label
              for="product-name"
              class="block text-sm font-semibold text-foreground mb-1.5"
            >
              Nombre <span class="text-red-500">*</span>
            </label>
            <input
              id="product-name"
              v-model="name"
              type="text"
              placeholder="Nombre del producto"
              :class="[
                'w-full h-11 px-4 border rounded-lg text-base transition-all duration-200',
                'focus:outline-none focus:ring-2 placeholder:text-muted-foreground',
                errors.name
                  ? 'border-red-400 focus:border-red-400 focus:ring-red-200'
                  : 'border-input focus:border-primary focus:ring-primary/20',
              ]"
            />
            <p v-if="errors.name" class="mt-1 text-xs text-red-500">
              {{ errors.name }}
            </p>
          </div>

          <!-- SKU + Barcode row -->
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label
                for="product-sku"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                SKU
              </label>
              <input
                id="product-sku"
                v-model="sku"
                type="text"
                placeholder="PROD-001"
                class="w-full h-11 px-4 border border-input rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground"
              />
            </div>
            <div>
              <label
                for="product-barcode"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                Código de barras
              </label>
              <input
                id="product-barcode"
                v-model="barcode"
                type="text"
                placeholder="7590000000000"
                class="w-full h-11 px-4 border border-input rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground"
              />
            </div>
          </div>

          <!-- Unit of measure -->
          <div>
            <label
              for="product-unit"
              class="block text-sm font-semibold text-foreground mb-1.5"
            >
              Unidad de medida
            </label>
            <select
              id="product-unit"
              v-model="unit"
              class="w-full h-11 px-4 border border-input rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
            >
              <option value="unidad">Unidad</option>
              <option value="kg">Kilogramo (kg)</option>
              <option value="g">Gramo (g)</option>
              <option value="l">Litro (l)</option>
              <option value="ml">Mililitro (ml)</option>
              <option value="caja">Caja</option>
              <option value="paquete">Paquete</option>
              <option value="saco">Saco</option>
            </select>
          </div>

          <!-- Price fields — conditional on fiscal type -->
          <div v-if="isFiscal" class="grid grid-cols-2 gap-4">
            <div>
              <label
                for="fiscal-price"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                Precio fiscal <span class="text-red-500">*</span>
              </label>
              <input
                id="fiscal-price"
                v-model="fiscalPrice"
                type="number"
                min="0"
                step="0.01"
                placeholder="0.00"
                :class="[
                  'w-full h-11 px-4 border rounded-lg text-base transition-all duration-200',
                  'focus:outline-none focus:ring-2 placeholder:text-muted-foreground',
                  errors.fiscalPrice
                    ? 'border-red-400 focus:border-red-400 focus:ring-red-200'
                    : 'border-input focus:border-primary focus:ring-primary/20',
                ]"
              />
              <p v-if="errors.fiscalPrice" class="mt-1 text-xs text-red-500">
                {{ errors.fiscalPrice }}
              </p>
            </div>
            <div>
              <label
                for="tax-rate"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                Tasa IVA <span class="text-red-500">*</span>
              </label>
              <Select
                id="tax-rate"
                v-model="taxRate"
                :options="taxRateOptions"
                placeholder="Seleccionar IVA..."
                :class="errors.taxRate ? 'border-red-400 ring-red-200' : ''"
              />
              <p v-if="errors.taxRate" class="mt-1 text-xs text-red-500">
                {{ errors.taxRate }}
              </p>
            </div>
          </div>

          <div v-else class="grid grid-cols-2 gap-4">
            <div>
              <label
                for="internal-price"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                Precio interno
                <span v-if="!usdPriceActive" class="text-red-500">*</span>
              </label>
              <input
                id="internal-price"
                v-model="internalPrice"
                type="number"
                min="0"
                step="0.01"
                placeholder="0.00"
                :disabled="usdPriceActive"
                :class="[
                  'w-full h-11 px-4 border rounded-lg text-base transition-all duration-200',
                  'focus:outline-none focus:ring-2 placeholder:text-muted-foreground',
                  'disabled:bg-gray-50 disabled:text-muted-foreground disabled:cursor-not-allowed',
                  errors.internalPrice
                    ? 'border-red-400 focus:border-red-400 focus:ring-red-200'
                    : 'border-input focus:border-primary focus:ring-primary/20',
                ]"
              />
              <p v-if="errors.internalPrice" class="mt-1 text-xs text-red-500">
                {{ errors.internalPrice }}
              </p>
              <p
                v-if="usdPriceActive"
                class="mt-1 text-xs text-muted-foreground"
              >
                Se calcula con la tasa del día (BCV) al guardar.
              </p>
            </div>
            <div>
              <label
                for="price-usd"
                class="block text-sm font-semibold text-foreground mb-1.5"
              >
                Precio en USD
              </label>
              <input
                id="price-usd"
                v-model="priceUsd"
                type="number"
                min="0"
                step="0.01"
                placeholder="0.00 (opcional)"
                :class="[
                  'w-full h-11 px-4 border rounded-lg text-base transition-all duration-200',
                  'focus:outline-none focus:ring-2 placeholder:text-muted-foreground',
                  errors.priceUsd
                    ? 'border-red-400 focus:border-red-400 focus:ring-red-200'
                    : 'border-input focus:border-primary focus:ring-primary/20',
                ]"
              />
              <p v-if="errors.priceUsd" class="mt-1 text-xs text-red-500">
                {{ errors.priceUsd }}
              </p>
            </div>
          </div>

          <!-- Category -->
          <div>
            <label
              for="category"
              class="block text-sm font-semibold text-foreground mb-1.5"
            >
              Categoría
            </label>
            <Select
              id="category"
              v-model="categoryId"
              :options="categoryOptions"
              placeholder="Sin categoría"
            />
          </div>

          <!-- Description -->
          <div>
            <label
              for="product-desc"
              class="block text-sm font-semibold text-foreground mb-1.5"
            >
              Descripción
            </label>
            <textarea
              id="product-desc"
              v-model="description"
              rows="3"
              placeholder="Descripción opcional del producto..."
              class="w-full px-4 py-3 border border-input rounded-lg text-base transition-all duration-200 focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20 placeholder:text-muted-foreground resize-none"
            />
          </div>

          <!-- Product image -->
          <div>
            <p class="text-sm font-semibold text-foreground mb-2">
              Imagen del producto
            </p>

            <!-- Show existing image (edit mode) or selected preview -->
            <div
              v-if="existingImageURL || imagePreviewURL"
              class="relative w-full rounded-xl overflow-hidden border bg-muted mb-3"
              style="aspect-ratio: 4/3"
            >
              <img
                :src="imagePreviewURL || existingImageURL"
                alt="Imagen del producto"
                class="w-full h-full object-cover"
              />
              <button
                type="button"
                title="Eliminar imagen"
                class="absolute top-2 right-2 w-8 h-8 rounded-full bg-red-500 text-white flex items-center justify-center shadow hover:bg-red-600 transition-colors cursor-pointer disabled:opacity-50"
                :disabled="removingImage"
                @click="
                  existingImageURL ? handleRemoveImage() : clearImageSelection()
                "
              >
                <Loader2
                  v-if="removingImage"
                  class="w-3.5 h-3.5 animate-spin"
                />
                <Trash2 v-else class="w-3.5 h-3.5" />
              </button>
            </div>

            <!-- Upload zone — shown when no image is set/selected -->
            <label
              v-if="!existingImageURL && !imagePreviewURL"
              class="flex flex-col items-center justify-center w-full rounded-xl border-2 border-dashed border-input bg-muted/40 cursor-pointer hover:bg-muted/60 transition-colors py-6 gap-2"
            >
              <ImagePlus class="w-7 h-7 text-muted-foreground" />
              <span class="text-sm text-muted-foreground font-medium">
                Arrastrar o hacer clic para subir imagen
              </span>
              <span class="text-xs text-muted-foreground">
                JPG, PNG o WebP — máx. 5 MB
              </span>
              <input
                ref="imageInputRef"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                class="sr-only"
                @change="onImageFileSelected"
              />
            </label>

            <!-- Replace button when image exists but user wants to change -->
            <label
              v-else
              class="inline-flex items-center gap-2 text-xs text-primary font-semibold cursor-pointer hover:underline mt-1"
            >
              <ImagePlus class="w-3.5 h-3.5" />
              Cambiar imagen
              <input
                ref="imageInputRef"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                class="sr-only"
                @change="onImageFileSelected"
              />
            </label>
          </div>

          <!-- Active toggle -->
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-semibold text-foreground">
                Estado del producto
              </p>
              <p class="text-xs text-muted-foreground mt-0.5">
                {{
                  active ? "Activo — visible en ventas" : "Inactivo — oculto"
                }}
              </p>
            </div>
            <Switch v-model="active" />
          </div>
        </div>

        <!-- Footer — sticky bottom -->
        <div
          class="flex-shrink-0 px-6 py-4 border-t border-border bg-white flex items-center justify-end gap-3 rounded-b-xl"
        >
          <button
            type="button"
            class="h-10 px-5 border border-input text-muted-foreground text-sm font-semibold rounded-lg hover:bg-gray-50 transition-all duration-200 cursor-pointer"
            @click="handleClose"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="saving"
            class="flex items-center gap-2 h-10 px-6 bg-primary text-white text-sm font-semibold rounded-lg hover:opacity-90 transition-all duration-200 cursor-pointer disabled:opacity-50"
            @click="handleSave"
          >
            <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
            <span>{{
              saving
                ? "Guardando..."
                : isEdit
                  ? "Guardar cambios"
                  : "Crear producto"
            }}</span>
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
