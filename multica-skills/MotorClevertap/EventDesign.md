# CleverTap Event Design — MyMotor

_Modules: 95 — Events: 156_

## Global Events - Mobile

### `screen_view`

**Trigger:** When any screen is viewed in the App

| Property | Type |
|---|---|
| `destination_screen` | string |
| `source_screen` | string |

### `modal_view`

**Trigger:** When any modal is viewed in the App

| Property | Type |
|---|---|
| `modal_name` | string |
| `source_screen` | string |

### `popup_view`

**Trigger:** Trigger when any popup is viewed in the App

| Property | Type |
|---|---|
| `id` | string |
| `popup_name` | string |

### `button_click`

**Trigger:** When any CTA button is clicked within in the App. Also contains certain parameters to breakdown based on specific button clicks (for ex if the button click is a home screen lottie animation - the param redirection would help with the redirect url)

| Property | Type |
|---|---|
| `button_name` | string |
| `challan_amount` | string |
| `challan_count` | number |
| `challan_nos` | string |
| `cl_no` | string |
| `redirection` | string |
| `reg_no` | string |
| `screen_name` | string |
| `total_amount` | string |
| `value` | string |
| `challanIds` | string |
| `doc_type` | string |
| `reg_number` | string |

## App Events - Mobile

### `app_uninstalled`

**Trigger:** When the app is uninstalled. Tracked by the BE placing a logic on silent push notif failure across 2 days. Could be replaced by default CT Event

| Property | Type |
|---|---|
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `reason` | string |
| `platform` | string |

### `app_install`

**Trigger:** When the app is installed. Could be replaced by default CT Event

| Property | Type |
|---|---|
| `version` | string |

## User Auth - Mobile

### `auth_initiate`

| Property | Type |
|---|---|
| `source_screen` | string |

### `login_success`

| Property | Type |
|---|---|
| `mode` | string |

### `signup_success`

| Property | Type |
|---|---|
| `mode` | string |

### `auth_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_details` | string |
| `error_message` | string |
| `mode` | string |
| `stack_trace` | string |
| `type` | string |

### `logout_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_details` | string |
| `error_message` | string |
| `mode` | string |
| `stack_trace` | string |
| `type` | string |

### `p_mobile_send_otp_error`

| Property | Type |
|---|---|
| `error_code` | number |
| `error_message` | string |
| `error_type` | string |
| `otp_flow` | string |
| `trace_id` | string |

### `p_mobile_verify_otp_error`

| Property | Type |
|---|---|
| `error_code` | number |
| `error_message` | string |
| `error_type` | string |
| `otp_flow` | string |
| `trace_id` | string |

### `oauth_email_faild (BE debug event)`

| Property | Type |
|---|---|
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |
| `reason` | string |
| `user_email` | string |
| `user_name` | string |
| `user_oauth_referance_pid` | string |

## RC Search - Mobile

### `rc_search_success`

| Property | Type |
|---|---|
| `manufacturer` | string |
| `mapper_make` | string |
| `mapper_model` | string |
| `mapper_variant` | string |
| `model_variant` | string |
| `retry_count` | string |
| `search_id` | string |
| `search_type` | string |
| `trace_id` | string |

### `rc_search_error`

| Property | Type |
|---|---|
| `error_code` | number |
| `error_message` | string |
| `error_type` | string |
| `retry_count` | string |
| `search_id` | string |
| `search_type` | string |
| `trace_id` | string |

### `rc_search_initiate`

| Property | Type |
|---|---|
| `screen_name` | string |

## Challan Search - Mobile

### `challan_search_success`

| Property | Type |
|---|---|
| `paid_challan` | number |
| `pending_challan` | number |
| `retry_count` | string |
| `search_id` | string |
| `search_type` | string |
| `trace_id` | string |

### `challan_search_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `retry_count` | string |
| `search_id` | string |
| `search_type` | string |
| `trace_id` | string |

### `challan_search_initiate`

| Property | Type |
|---|---|
| `screen_name` | string |

## Challan Payment - Mobile

### `challan_init_payment_success`

| Property | Type |
|---|---|
| `challan_nos` | string |
| `trace_id` | string |
| `vehicle_no` | string |

### `challan_webview_exit_success`

| Property | Type |
|---|---|
| `reg_no` | string |
| `screen_name` | string |
| `url` | string |

### `challan_check_eligibility_success`

| Property | Type |
|---|---|
| `challan_nos` | string |
| `trace_id` | string |
| `vehicle_no` | string |

### `payment_inprocess_app`

| Property | Type |
|---|---|
| `amount` | string |
| `app_version` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

### `challan_init_payment_error`

| Property | Type |
|---|---|
| `challan_nos` | string |
| `error_code` | number |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |
| `vehicle_no` | string |

### `challan_check_eligibility_error`

| Property | Type |
|---|---|
| `challan_nos` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |
| `vehicle_no` | string |

## Payment Success - App

### `payment_success_app`

| Property | Type |
|---|---|
| `amount` | string |
| `app_version` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Garage - Mobile

### `add_vehicle_garage_success`

| Property | Type |
|---|---|
| `garage_id` | string |
| `trace_id` | string |
| `vehicle_id` | string |

### `add_vehicle_garage_initiate`

| Property | Type |
|---|---|
| `source_screen` | string |

### `remove_vehicle_from_garage_success`

| Property | Type |
|---|---|
| `garage_id` | string |
| `trace_id` | string |
| `vehicle_id` | string |

### `add_vehicle_garage_error`

| Property | Type |
|---|---|
| `error_code` | number |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

### `garage_fetch_error (FE debug event)`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `garage_id` | string |
| `trace_id` | string |

### `remove_vehicle_garage_initiate`

| Property | Type |
|---|---|
| `garage_id` | string |
| `source_screen` | string |

### `garage_fetch_success (FE debug event)`

| Property | Type |
|---|---|
| `garage_id` | string |
| `trace_id` | string |

## Glovebox - Mobile

### `dgl_fetch_base64_success`

| Property | Type |
|---|---|
| `trace_id` | string |

## Virtual RC - Mobile

### `export_rc_data_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `source_screen` | string |

### `export_rc_data_success`

| Property | Type |
|---|---|
| `source_screen` | string |

## Insurance - Mobile

### `insurance_weblink_opened`

| Property | Type |
|---|---|
| `reg_no` | string |
| `source_screen` | string |
| `vehicle_class` | string |

## User Feedback - Mobile

### `feedback_logged`

| Property | Type |
|---|---|
| `component_id` | string |
| `created_at` | datetime |
| `displayed_times` | number |
| `rating` | number |
| `updated_at` | datetime |
| `feedback_model` | string |

## Global Events - Web

### `page_view`

| Property | Type |
|---|---|
| `device_os` | string |
| `path` | string |
| `screen_name` | string |
| `title` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |
| `user_id` | string |

## User Auth - Web

### `signup_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `mode` | string |
| `user_id` | string |
| `user_status` | string |

## Challan Payment - Web

### `payment_inprocess_web`

| Property | Type |
|---|---|
| `amount` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

### `challan_payment_redirection_web`

| Property | Type |
|---|---|
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `is_in_process` | boolean |
| `vehicle_number` | string |
| `browser` | string |
| `platform` | string |
| `user_id` | string |

## Challan Search - Web

### `challan_search_initiate_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |

### `challan_search_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |

### `challan_search_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `user_id` | string |
| `user_status` | string |

### `anon_challan_rc_search_error_web`

**Trigger:** Deprecated Event / Param since this feature would be discontinued as we would not allow anonymous searches anymore

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `user_id` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |

### `anon_challan_rc_search_success_web`

**Trigger:** Deprecated Event / Param since this feature would be discontinued as we would not allow anonymous searches anymore

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |

## Promotion Clicked

### `promotion_clicked`

| Property | Type |
|---|---|
| `banner_id` | string |
| `source_screen` | string |

## Vehicle Mapper

### `vehicle_mapper_image_err`

| Property | Type |
|---|---|
| `error` | string |
| `mapper_make` | string |
| `mapper_model` | string |

## Fuel Price

### `fuel_price_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `state_name_passed` | string |
| `trace_id` | string |

## Login Success - Web

### `login_success_web`

| Property | Type |
|---|---|
| `_internal` | object |
| `device_os` | string |
| `mode` | string |
| `user_id` | string |
| `user_status` | string |

## Auth Initiate - Web

### `auth_initiate_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `mode` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |

## Dgl Redirect

### `dgl_redirect_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `trace_id` | string |

### `dgl_redirect_success`

| Property | Type |
|---|---|
| `trace_id` | string |

## Fastag Details

### `fastag_details_fetch_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `reg_no` | string |
| `trace_id` | string |
| `error_type` | string |

### `fastag_details_fetch_success`

| Property | Type |
|---|---|
| `reg_no` | string |
| `trace_id` | string |

## Glovebox Doc

### `glovebox_doc_share_initiate`

| Property | Type |
|---|---|
| `doc_type` | string |
| `reg_no` | string |

## Insurance Weblink - Web

### `insurance_weblink_opened_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `rc_number` | string |
| `screen_name` | string |
| `url` | string |
| `user_id` | string |
| `user_status` | string |
| `vehicle_class` | string |

## Auth Error - Web

### `auth_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `mode` | string |
| `user_id` | string |
| `user_status` | string |

## Challan Webview

### `challan_webview_exit_failure`

| Property | Type |
|---|---|
| `reg_no` | string |
| `screen_name` | string |
| `url` | string |

## P Mobile

### `p_mobile_send_otp_success`

| Property | Type |
|---|---|
| `otp_flow` | string |
| `trace_id` | string |

### `p_mobile_verify_otp_success`

| Property | Type |
|---|---|
| `otp_flow` | string |
| `trace_id` | string |

## RC SeaRCh - Web

### `rc_search_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `user_id` | string |
| `user_status` | string |

### `rc_search_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |

## Fasttag Initiate

### `fasttag_initiate`

_No custom properties (CT default params only)._

## Dgl Fetch

### `dgl_fetch_docs_success`

| Property | Type |
|---|---|
| `trace_id` | string |

### `dgl_fetch_base64_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `trace_id` | string |

### `dgl_fetch_docs_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Anon RC - Web

### `anon_rc_search_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |

### `anon_rc_search_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `user_id` | string |
| `user_status` | string |

## Challan Init - Web

### `challan_init_payment_success_web`

| Property | Type |
|---|---|
| `api_response_status` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `has_payment_url` | boolean |
| `is_in_process` | boolean |
| `vehicle_number` | string |
| `browser` | string |
| `platform` | string |
| `user_id` | string |

### `challan_init_payment_error_web`

| Property | Type |
|---|---|
| `api_response` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `error_message` | string |
| `error_type` | string |
| `is_in_process` | boolean |
| `payment_status` | string |
| `vehicle_number` | string |

## Webview Init

### `webview_init`

| Property | Type |
|---|---|
| `source_screen` | string |
| `url` | string |

## Button Click

### `Button Click`

| Property | Type |
|---|---|
| `device_os` | string |
| `event_name` | string |
| `page_url` | string |
| `screen_name` | string |
| `source` | string |
| `user_id` | string |
| `user_status` | string |
| `vehicle_number` | string |
| `event_screen` | string |
| `expiry_date` | string |
| `field_name` | string |
| `insurance_company` | string |
| `insurance_valid_upto` | string |
| `policy_number` | string |
| `status` | string |
| `emission_norm` | string |
| `rc_pucc_no` | string |

## Login Success

### `Login Success`

_No custom properties (CT default params only)._

## Pay Challan - Web

### `pay_challan_button_clicked_web`

| Property | Type |
|---|---|
| `challan_count` | number |
| `challan_numbers` | list |
| `total_amount` | number |
| `vehicle_number` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `is_in_process` | boolean |
| `browser` | string |
| `platform` | string |
| `user_id` | string |

## Payment Failed - App

### `payment_failed_app`

| Property | Type |
|---|---|
| `amount` | string |
| `app_version` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## App Updated

### `app_updated`

| Property | Type |
|---|---|
| `from_version` | string |
| `to_version` | string |

## Button Click - Web

### `button_click_web`

| Property | Type |
|---|---|
| `button_name` | string |
| `device_os` | string |
| `screen_name` | string |
| `search_type` | string |
| `user_id` | string |
| `user_status` | string |
| `vehicle_number` | string |

## Challan Check - Web

### `challan_check_eligibility_success_web`

| Property | Type |
|---|---|
| `api_response_status` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `is_in_process` | boolean |
| `is_payable` | boolean |
| `payable_count` | number |
| `total_challans_checked` | number |
| `vehicle_number` | string |
| `from_cache` | boolean |
| `browser` | string |
| `platform` | string |
| `user_id` | string |

## Challan RC - Web

### `challan_rc_search_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `user_id` | string |
| `user_status` | string |

### `challan_rc_search_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |
| `browser` | string |
| `platform` | string |

## Challan SeaRCh

### `challan_search_history_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

### `challan_search_history_success`

| Property | Type |
|---|---|
| `trace_id` | string |

## Court Preference

### `court_preference_selected`

| Property | Type |
|---|---|
| `preference` | string |
| `vehicle_number` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `is_in_process` | boolean |

## Deep Link

### `deep_link_opened`

| Property | Type |
|---|---|
| `host` | string |
| `link_type` | string |
| `path` | string |
| `source` | string |

## Delete Custom

### `delete_custom_data_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## Delete Document

### `delete_document_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

### `delete_document_success`

| Property | Type |
|---|---|
| `trace_id` | string |

## Dgl Get

### `dgl_get_issued_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Dgl Pull

### `dgl_pull_doc_error`

| Property | Type |
|---|---|
| `doc_type` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `org_id` | string |
| `trace_id` | string |

### `dgl_pull_doc_success`

| Property | Type |
|---|---|
| `doc_type` | string |
| `org_id` | string |
| `trace_id` | string |

## Dgl Supported

### `dgl_supported_docs_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Export Error

### `export_error`

| Property | Type |
|---|---|
| `error` | string |
| `invocation` | string |
| `remarks` | string |

## Fetch All

### `fetch_all_org_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## Fetch Custom

### `fetch_custom_data_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Fetch Faqs

### `fetch_faqs_error`

| Property | Type |
|---|---|
| `error_message` | string |

## Get All

### `get_all_fetched_docs_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Insurance Expired

### `insurance_expired_alert`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `insurer_name` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Insurance Expiry

### `insurance_expiry_alert_1d`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `insurer_name` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

### `insurance_expiry_alert_30d`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `insurer_name` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

### `insurance_expiry_alert_60d`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `insurer_name` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Insurance RC

### `insurance_rc_search_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## Insurance RC - Web

### `insurance_rc_search_error_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `page_url` | string |
| `rc_number` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |

### `insurance_rc_search_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `rc_number` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |
| `vehicle_class` | string |

## Logout Success

### `logout_success`

| Property | Type |
|---|---|
| `mode` | string |

## Menu Navigation

### `menu_navigation`

| Property | Type |
|---|---|
| `source` | string |

## Oauth Combination

### `oauth_combination_faild`

| Property | Type |
|---|---|
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |
| `reason` | string |
| `user_email` | string |
| `user_name` | string |
| `user_oauth_referance_pid` | string |

## Oauth Referanceid

### `oauth_referanceid_faild`

| Property | Type |
|---|---|
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |
| `reason` | string |
| `user_email` | string |
| `user_name` | string |
| `user_oauth_referance_pid` | string |

## Open Recent

### `open_recent_search`

| Property | Type |
|---|---|
| `search_type` | string |

## Order Details

### `order_details_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## Payment Failed - Web

### `payment_failed_web`

| Property | Type |
|---|---|
| `amount` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Payment Not

### `payment_not_payable`

| Property | Type |
|---|---|
| `api_response_status` | string |
| `challan_amount` | string |
| `challan_number` | string |
| `challan_status` | string |
| `is_in_process` | boolean |
| `is_payable` | boolean |
| `payable_count` | number |
| `total_challans_checked` | number |
| `vehicle_number` | string |
| `from_cache` | boolean |

## Payment Refund - App

### `payment_refund_processing_app`

| Property | Type |
|---|---|
| `amount` | string |
| `app_version` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Payment Refund - Web

### `payment_refund_processing_web`

| Property | Type |
|---|---|
| `amount` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Payment Refunded - App

### `payment_refunded_app`

| Property | Type |
|---|---|
| `amount` | string |
| `app_version` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Payment Refunded - Web

### `payment_refunded_web`

| Property | Type |
|---|---|
| `amount` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Payment Result

### `payment_result_viewed`

| Property | Type |
|---|---|
| `is_authenticated` | boolean |
| `reason` | string |
| `success` | boolean |

## Payment Success - Web

### `payment_success_web`

| Property | Type |
|---|---|
| `amount` | string |
| `challan_no` | string |
| `challan_order_id` | string |
| `device_platform` | string |
| `dynamic_title` | string |
| `event_name` | string |
| `order_id` | string |
| `order_status` | string |
| `paid_on` | datetime |
| `payment_status` | string |
| `reason` | string |
| `reg_no` | string |
| `settlement_amount` | string |
| `transaction_id` | string |
| `user_pid` | string |

## Popup Dismissed

### `popup_dismissed`

| Property | Type |
|---|---|
| `id` | string |
| `popup_name` | string |

## Pricing Slabs

### `pricing_slabs_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `trace_id` | string |

## Puc Expired

### `puc_expired_alert`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## Puc Expiry

### `puc_expiry_alert_1d`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

### `puc_expiry_alert_30d`

| Property | Type |
|---|---|
| `expiry_date` | string |
| `reason` | string |
| `vehicle_garage_pid` | string |
| `vehicle_manufacturer` | string |
| `vehicle_model` | string |
| `vehicle_rc` | string |
| `app_version` | string |
| `build_number` | string |
| `cp_user_pid` | string |
| `device_pid` | string |
| `platform` | string |

## RC SeaRCh

### `rc_search_history_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `trace_id` | string |

### `rc_search_history_success`

| Property | Type |
|---|---|
| `trace_id` | string |

### `rc_search_submit`

_No custom properties (CT default params only)._

## Remove Vehicle

### `remove_vehicle_from_garage_error`

| Property | Type |
|---|---|
| `error_code` | number |
| `error_type` | string |
| `error_message` | string |
| `garage_id` | string |
| `trace_id` | string |

## Supported Docs

### `supported_docs_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## Transaction History

### `transaction_history_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `error_message` | string |
| `trace_id` | string |

## Update Custom

### `update_custom_data_error`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |
| `od_expiry` | string |
| `od_provider` | string |
| `trace_id` | string |
| `tp_expiry` | string |
| `tp_provider` | string |

### `update_custom_data_success`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_message` | string |
| `error_type` | string |
| `trace_id` | string |

## User Account

### `user_account_deletion_err`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

### `user_account_deletion_init`

| Property | Type |
|---|---|
| `deletion_feedback` | string |
| `deletion_reason` | string |

## User Details

### `user_details_err`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## User Location

### `user_location_fetch_error`

| Property | Type |
|---|---|
| `error` | string |

## User Logout - Web

### `user_logout_success_web`

| Property | Type |
|---|---|
| `device_os` | string |
| `page_url` | string |
| `screen_name` | string |
| `user_id` | string |
| `user_status` | string |

## Verify Token

### `verify_token_err`

| Property | Type |
|---|---|
| `error_code` | string |
| `error_type` | string |

## Webview Exit

### `webview_exit`

| Property | Type |
|---|---|
| `url` | string |

## Challan SeaRCh Exception

### `Challan Search Exception`

_No custom properties (CT default params only)._

## Challan SeaRCh Failed

### `Challan Search Failed`

_No custom properties (CT default params only)._

## RC SeaRCh Exception

### `RC Search Exception`

_No custom properties (CT default params only)._

## RC SeaRCh Failed

### `RC Search Failed`

_No custom properties (CT default params only)._

## RC SeaRCh Success

### `RC Search Success`

_No custom properties (CT default params only)._

## User Logout

### `user_logout_success`

| Property | Type |
|---|---|
| `name` | string |
| `email` | string |
| `zoop_user_id` | string |
| `google_uuid` | string |
| `email_verified` | string |
| `user_state` | string |
| `user_city` | string |
| `garage_vehicle_count` | string |
| `mobile` | string |
| `browser` | string |
| `platform` | string |
| `initial_utm_campaign_id` | unknown |
| `initial_utm_creative_format` | unknown |
| `initial_utm_id` | string |
| `initial_utm_marketing_tactic` | unknown |
| `initial_utm_source_platform` | unknown |
| `user_id` | string |

## Clevertap (CT) Default Events

### `App Installed`

**Trigger:** When the App is Installed

_No custom properties (CT default params only)._

### `App Launched`

**Trigger:** When the App is Launched

_No custom properties (CT default params only)._

### `App Uninstalled`

**Trigger:** When the App is Uninstalled from user device

_No custom properties (CT default params only)._

### `Notification Sent`

**Trigger:** When a notification campaign is sent to the user

_No custom properties (CT default params only)._

### `Notification Viewed`

**Trigger:** When a notification campaign is viewed by the user

_No custom properties (CT default params only)._

### `Notification Clicked`

**Trigger:** When a notification campaign is clicked by the user

_No custom properties (CT default params only)._

### `Identity Set`

**Trigger:** When a user's profile identity is set in CT

_No custom properties (CT default params only)._

### `Identity Reset`

**Trigger:** When a user's profile identity is reset in CT

_No custom properties (CT default params only)._

### `Identity Error`

**Trigger:** When a user's profile identity could not be set in CT

_No custom properties (CT default params only)._

### `First App Launch?`

_No custom properties (CT default params only)._

