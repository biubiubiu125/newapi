/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useState, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { toastUnhandledConsoleError } from '@/lib/handle-server-error'
import { localizeConsoleErrorText } from '@/lib/server-error-message'
import * as z from 'zod'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { JsonEditor } from '@/components/json-editor'
import { TagInput } from '@/components/tag-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { ModelPricingPanel } from '@/features/model-pricing/model-pricing-panel'

import { createModel, updateModel, getModel, getVendors } from '../../api'
import { getNameRuleOptions, ENDPOINT_TEMPLATES } from '../../constants'
import { modelsQueryKeys, vendorsQueryKeys, parseModelTags } from '../../lib'
import type { Model } from '../../types'
import { ModelConnections } from '../model-connections'

const extendedModelFormSchema = z.object({
  id: z.number().optional(),
  model_name: z.string().min(1, 'Model name is required'),
  description: z.string(),
  icon: z.string(),
  tags: z.array(z.string()),
  vendor_id: z.number().optional(),
  endpoints: z.string(),
  name_rule: z.number(),
  status: z.boolean(),
  sync_official: z.boolean(),
})

type ExtendedModelFormValues = z.infer<typeof extendedModelFormSchema>

type ModelMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Model | null
  initialSection?: string
}

export function ModelMutateDrawer({
  open,
  onOpenChange,
  currentRow,
  initialSection = 'metadata',
}: ModelMutateDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isEditing = Boolean(currentRow?.id)
  const hasModelName = Boolean(currentRow?.model_name)
  const [section, setSection] = useState(initialSection)
  const [isSubmitting, setIsSubmitting] = useState(false)

  // Fetch vendors for dropdown
  const { data: vendorsData } = useQuery({
    queryKey: vendorsQueryKeys.list(),
    queryFn: () => getVendors({ page_size: 1000 }),
    enabled: open,
  })

  const vendors = vendorsData?.data?.items || []

  // Fetch model detail if editing
  const { data: modelData } = useQuery({
    queryKey: modelsQueryKeys.detail(currentRow?.id || 0),
    queryFn: () => getModel(currentRow!.id),
    enabled: open && isEditing,
  })

  const form = useForm<ExtendedModelFormValues>({
    resolver: zodResolver(extendedModelFormSchema),
    defaultValues: {
      model_name: '',
      description: '',
      icon: '',
      tags: [],
      vendor_id: undefined,
      endpoints: '',
      name_rule: 0,
      status: true,
      sync_official: true,
    },
  })

  useEffect(() => {
    if (!open) return
    setSection(initialSection)
  }, [open, initialSection, currentRow?.id, currentRow?.model_name])

  useEffect(() => {
    if (open && isEditing && modelData?.data) {
      const model = modelData.data
      form.reset({
        id: model.id,
        model_name: model.model_name,
        description: model.description || '',
        icon: model.icon || '',
        tags: parseModelTags(model.tags),
        vendor_id: model.vendor_id,
        endpoints: model.endpoints || '',
        name_rule: model.name_rule || 0,
        status: model.status === 1,
        sync_official: model.sync_official === 1,
      })
    } else if (open && !isEditing) {
      form.reset({
        model_name: currentRow?.model_name || '',
        description: '',
        icon: '',
        tags: [],
        vendor_id: undefined,
        endpoints: '',
        name_rule: 0,
        status: true,
        sync_official: true,
      })
    }
  }, [open, isEditing, modelData, currentRow, form])

  const onSubmit = useCallback(
    async (values: ExtendedModelFormValues): Promise<void> => {
      setIsSubmitting(true)
      try {
        const modelData = {
          ...values,
          tags: Array.isArray(values.tags) ? values.tags.join(',') : '',
          status: values.status ? 1 : 0,
          sync_official: values.sync_official ? 1 : 0,
        }

        const response = isEditing
          ? await updateModel({ ...modelData, id: currentRow!.id })
          : await createModel(modelData)

        if (response.success) {
          toast.success(
            isEditing
              ? t('Model updated successfully')
              : t('Model created successfully')
          )
          queryClient.invalidateQueries({ queryKey: modelsQueryKeys.lists() })
          queryClient.invalidateQueries({ queryKey: ['system-options'] })
          onOpenChange(false)
        } else {
          toast.error(
            response.message?.trim()
              ? localizeConsoleErrorText(response.message)
              : t('Operation failed')
          )
        }
      } catch (error: unknown) {
        toastUnhandledConsoleError(error)
      } finally {
        setIsSubmitting(false)
      }
    },
    [isEditing, currentRow, queryClient, onOpenChange, t]
  )

  const handleFillEndpointTemplate = (templateKey: string) => {
    const template = ENDPOINT_TEMPLATES[templateKey]
    if (template) {
      const templateJson = JSON.stringify({ [templateKey]: template }, null, 2)
      form.setValue('endpoints', templateJson)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='pr-6 break-all'>
            {hasModelName ? currentRow?.model_name : t('Create Model')}
          </SheetTitle>
          <SheetDescription>
            {isEditing
              ? t("Update model configuration and click save when you're done.")
              : t(
                  'Add a new model to the system by providing the necessary information.'
                )}
          </SheetDescription>
        </SheetHeader>

        <Tabs
          value={section}
          onValueChange={setSection}
          className='flex min-h-0 flex-1 flex-col'
        >
          <TabsList className='mx-4 w-auto self-start'>
            <TabsTrigger value='metadata'>{t('Metadata')}</TabsTrigger>
            <TabsTrigger value='pricing' disabled={!hasModelName}>
              {t('Pricing')}
            </TabsTrigger>
            <TabsTrigger value='connections' disabled={!hasModelName}>
              {t('Channels and groups')}
            </TabsTrigger>
          </TabsList>
          <TabsContent
            value='metadata'
            className='flex min-h-0 flex-1 flex-col overflow-hidden'
          >
            <Form {...form}>
              <form
                id='model-form'
                onSubmit={form.handleSubmit(
                  onSubmit as Parameters<typeof form.handleSubmit>[0]
                )}
                className={sideDrawerFormClassName()}
              >
                {/* Basic Information */}
                <SideDrawerSection>
                  <h3 className='text-sm font-semibold'>
                    {t('Basic Information')}
                  </h3>

                  <FormField
                    control={form.control}
                    name='model_name'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Model Name *')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t('gpt-4, claude-3-opus, etc.')}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('The unique identifier for this model')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='description'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Description')}</FormLabel>
                        <FormControl>
                          <Textarea
                            placeholder={t('Describe this model...')}
                            rows={3}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='icon'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Icon')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t('OpenAI, Anthropic, etc.')}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription className='text-xs'>
                          {t('@lobehub/icons key')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='vendor_id'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Vendor')}</FormLabel>
                        <Select
                          items={vendors.map((vendor) => ({
                            value: String(vendor.id),
                            label: vendor.name,
                          }))}
                          onValueChange={(value) =>
                            field.onChange(
                              value ? Number.parseInt(value) : undefined
                            )
                          }
                          value={field.value ? String(field.value) : undefined}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue placeholder={t('Select vendor')} />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {vendors.map((vendor) => (
                                <SelectItem
                                  key={vendor.id}
                                  value={String(vendor.id)}
                                >
                                  {vendor.name}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='tags'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Tags')}</FormLabel>
                        <FormControl>
                          <TagInput
                            value={field.value || []}
                            onChange={field.onChange}
                            placeholder={t('Add tags...')}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Press Enter or comma to add tags')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SideDrawerSection>

                {/* Matching Configuration */}
                <SideDrawerSection>
                  <h3 className='text-sm font-semibold'>
                    {t('Matching Rules')}
                  </h3>

                  <FormField
                    control={form.control}
                    name='name_rule'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Name Rule')}</FormLabel>
                        <FormControl>
                          <RadioGroup
                            onValueChange={(value) =>
                              field.onChange(Number.parseInt(value))
                            }
                            value={String(field.value)}
                            className='grid grid-cols-2 gap-4'
                          >
                            {getNameRuleOptions(t).map((option) => (
                              <div
                                key={option.value}
                                className='flex items-center space-x-2'
                              >
                                <RadioGroupItem
                                  value={String(option.value)}
                                  id={`rule-${option.value}`}
                                />
                                <Label
                                  htmlFor={`rule-${option.value}`}
                                  className='cursor-pointer font-normal'
                                >
                                  {option.label}
                                </Label>
                              </div>
                            ))}
                          </RadioGroup>
                        </FormControl>
                        <FormDescription>
                          {t('How this model name should match requests')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SideDrawerSection>

                {/* Endpoints Configuration */}
                <SideDrawerSection>
                  <div className='flex items-center justify-between'>
                    <h3 className='text-sm font-semibold'>{t('Endpoints')}</h3>
                    <Select<string>
                      items={Object.keys(ENDPOINT_TEMPLATES).map((key) => ({
                        value: key,
                        label: key,
                      }))}
                      onValueChange={(v) =>
                        v !== null && handleFillEndpointTemplate(v)
                      }
                    >
                      <SelectTrigger size='sm' className='w-[200px]'>
                        <SelectValue placeholder={t('Load template...')} />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {Object.keys(ENDPOINT_TEMPLATES).map((key) => (
                            <SelectItem key={key} value={key}>
                              {key}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>

                  <FormField
                    control={form.control}
                    name='endpoints'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Endpoint Configuration')}</FormLabel>
                        <FormControl>
                          <JsonEditor
                            value={field.value || ''}
                            onChange={field.onChange}
                            keyPlaceholder='endpoint_type'
                            valuePlaceholder='{"path": "/v1/...", "method": "POST"}'
                            keyLabel='Endpoint Type'
                            valueLabel='Configuration'
                            valueType='any'
                            emptyMessage={t(
                              'No endpoints configured. Switch to JSON mode or add rows to define endpoints.'
                            )}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Define API endpoints for this model (JSON format)'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SideDrawerSection>

                {/* Status & Sync */}
                <SideDrawerSection>
                  <h3 className='text-sm font-semibold'>
                    {t('Status & Sync')}
                  </h3>

                  <FormField
                    control={form.control}
                    name='status'
                    render={({ field }) => (
                      <FormItem className={sideDrawerSwitchItemClassName()}>
                        <div className='flex flex-col gap-0.5'>
                          <FormLabel className='text-base'>
                            {t('Enabled')}
                          </FormLabel>
                          <FormDescription>
                            {t('Enable or disable this model')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='sync_official'
                    render={({ field }) => (
                      <FormItem className={sideDrawerSwitchItemClassName()}>
                        <div className='flex flex-col gap-0.5'>
                          <FormLabel className='text-base'>
                            {t('Official Sync')}
                          </FormLabel>
                          <FormDescription>
                            {t('Sync this model with official upstream')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                </SideDrawerSection>
              </form>
            </Form>
          </TabsContent>
          <TabsContent
            value='pricing'
            className='flex min-h-0 flex-1 flex-col overflow-hidden'
          >
            {hasModelName && currentRow?.model_name ? (
              <ModelPricingPanel modelName={currentRow.model_name} />
            ) : null}
          </TabsContent>
          <TabsContent
            value='connections'
            className='flex min-h-0 flex-1 flex-col overflow-hidden'
          >
            {currentRow ? <ModelConnections model={currentRow} /> : null}
          </TabsContent>
        </Tabs>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' disabled={isSubmitting} />}
          >
            {t('Cancel')}
          </SheetClose>
          {section !== 'pricing' ? (
            <Button form='model-form' type='submit' disabled={isSubmitting}>
              {isSubmitting && (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              )}
              {isEditing ? t('Update Model') : t('Save metadata')}
            </Button>
          ) : null}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
