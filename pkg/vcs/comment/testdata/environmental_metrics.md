<h3>💰 Infracost report</h3>

<p>Consider fixing this issue, it doesn't align with your company's FinOps policies & the Well-Architected Framework. <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>

<table>
  <tr><td colspan="2" width="1000px">FinOps policies</td></tr>
  
<tr><td colspan="2" title="Failure">
<details >
<summary>
<b>🔴 Use Graviton instances</b>
</summary><br/>

Graviton instances are more energy efficient.
</details>
</td></tr>
<tr><td></td><td>

`aws_instance.web`
  * Switch to Graviton instance type
    * 💰 save $600/year
    * 🌱 avoid 2.40 t CO₂e - that's more than 16 flights between London & Paris
    * 🔧 [Fix in your IDE](https://cost.dev/?utm_source=pr_comment&utm_content=fix_in_ide) — or ask your agent to apply it with Infracost Dev
</td></tr>

  </table>
<details >
  <summary><b>Monthly estimate increased by $100 📈</b> (🌱 emits 500.0 kg CO₂e - that's more than 3.3 flights between London & Paris)</summary>
  <br/>

<table>
  <thead>
    <td>Changed project</td>
    <td><span title="Baseline costs are consistent charges for provisioned resources, like the hourly cost for a virtual machine, which stays constant no matter how much it is used. Infracost estimates these resources assuming they are used for the whole month (730 hours).">Baseline cost</span></td>
    <td><span title="Usage costs are charges based on actual usage, like the storage cost for an object storage bucket. Infracost estimates these resources using the monthly usage values in the usage-file.">Usage cost</span>*</td>
    <td>Total change</td>
    <td>New monthly cost</td>
  </thead>
  <tbody>
    <tr>
      <td>my-project</td>
      <td align="right">+$100</td>
      <td align="right">-</td>
      <td align="right">+$100 (+50%)</td>
      <td align="right">$300</td>
    </tr>
  </tbody>
</table>


*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.

**The methodology for calculating the CO₂e impact of your changes is explained [in our docs](https://www.infracost.io/docs/infracost_cloud/infracarbon).
  <details>
  <summary>Estimate details </summary>

```
Key: * usage cost, ~ changed, + added, - removed

──────────────────────────────────
Project: my-project

~ aws_instance.web
  +$100 ($200 → $300)

Monthly cost change for my-project
Amount:  +$100 ($200 → $300)
Percent: +50%

──────────────────────────────────
Key: * usage cost, ~ changed, + added, - removed

*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.

1 cloud resource was detected:
∙ 1 was estimated
```
  </details>

</details>

<hr/>

<hr/>

![Infracost Dev](https://img.shields.io/badge/Infracost-Dev-db2777?labelColor=000)

**Let your coding agent remediate these.** Infracost Dev gives Cursor, Claude Code and Copilot your FinOps policies and live cloud pricing — so the next PR ships clean.

[cost.dev](https://cost.dev/?utm_source=pr_comment&utm_content=infracost_dev_promo) · [Setup guide](https://www.infracost.io/docs/?utm_source=pr_comment&utm_content=infracost_dev_promo)

<sub>
  This comment will be updated when code changes.
</sub>

