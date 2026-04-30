"use client";

import { useState } from "react";
import { Building2, X as XIcon } from "lucide-react";
import { toast } from "sonner";
import { useCreateCustomer } from "@multica/core/customers/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import type { CustomerStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useNavigation } from "../navigation";

function TextField({
  label,
  value,
  onChange,
  placeholder,
  autoFocus,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-muted-foreground">{label}</span>
      <input
        autoFocus={autoFocus}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
      />
    </label>
  );
}

export function CreateCustomerModal({ onClose }: { onClose: () => void }) {
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const createCustomer = useCreateCustomer();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [website, setWebsite] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [status, setStatus] = useState<CustomerStatus>("active");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    const trimmedName = name.trim();
    if (!trimmedName || submitting) return;
    setSubmitting(true);
    try {
      const customer = await createCustomer.mutateAsync({
        name: trimmedName,
        description: description.trim() || undefined,
        website: website.trim() || undefined,
        email: email.trim() || undefined,
        phone: phone.trim() || undefined,
        status,
      });
      onClose();
      toast.success("Customer created");
      router.push(wsPaths.customerDetail(customer.id));
    } catch {
      toast.error("Failed to create customer");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent showCloseButton={false} className="max-w-lg p-0 gap-0 overflow-hidden">
        <DialogTitle className="sr-only">New customer</DialogTitle>
        <div className="flex items-center justify-between border-b px-5 py-3">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Building2 className="h-4 w-4 text-muted-foreground" />
            New customer
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all"
          >
            <XIcon className="size-4" />
          </button>
        </div>
        <div className="space-y-4 px-5 py-4">
          <TextField label="Name" value={name} onChange={setName} placeholder="Acme Inc" autoFocus />
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted-foreground">Status</span>
            <select
              value={status}
              onChange={(event) => setStatus(event.target.value as CustomerStatus)}
              className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="active">Active</option>
              <option value="archived">Archived</option>
            </select>
          </label>
          <TextField label="Website" value={website} onChange={setWebsite} placeholder="https://example.com" />
          <div className="grid grid-cols-2 gap-3">
            <TextField label="Email" value={email} onChange={setEmail} placeholder="hello@example.com" />
            <TextField label="Phone" value={phone} onChange={setPhone} placeholder="+1 555 0100" />
          </div>
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted-foreground">Notes</span>
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Internal notes"
              className="min-h-24 w-full resize-none rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
          </label>
        </div>
        <div className="flex justify-end gap-2 border-t px-5 py-3">
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button onClick={handleSubmit} disabled={!name.trim() || submitting}>
            Create customer
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
