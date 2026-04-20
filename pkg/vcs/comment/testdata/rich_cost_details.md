<h3>💰 Infracost report</h3>
This pull request is aligned with your company's FinOps policies and the Well-Architected Framework.
<details >
  <summary><b>Monthly estimate increased by €300 📈</b> </summary>
  <br/>

<table>
  <thead>
    <td>Changed project</td>
    <td>Module path</td>
    <td><span title="Baseline costs are consistent charges for provisioned resources, like the hourly cost for a virtual machine, which stays constant no matter how much it is used. Infracost estimates these resources assuming they are used for the whole month (730 hours).">Baseline cost</span></td>
    <td><span title="Usage costs are charges based on actual usage, like the storage cost for an object storage bucket. Infracost estimates these resources using the monthly usage values in the usage-file.">Usage cost</span>*</td>
    <td>Total change</td>
    <td>New monthly cost</td>
  </thead>
  <tbody>
    <tr>
      <td>my-project</td>
      <td>modules/compute</td>
      <td align="right">+€190</td>
      <td align="right">+€10</td>
      <td align="right">+€200 (+67%)</td>
      <td align="right">€500</td>
    </tr>
    <tr>
      <td>my-project</td>
      <td>modules/network</td>
      <td align="right">+€100</td>
      <td align="right">-</td>
      <td align="right">+€100 (+50%)</td>
      <td align="right">€300</td>
    </tr>
  </tbody>
</table>


*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.
  <details>
  <summary>Estimate details (includes details of unsupported resources)</summary>

```
Key: * usage cost, ~ changed, + added, - removed

──────────────────────────────────
Project: my-project
Module path: modules/compute
Workspace: prod

~ aws_instance.web
  +€180 (€300 → €480)

    ~ Compute (on-demand, m5.xlarge)
      +€70 (€70 → €140)

    ~ root_block_device

        ~ Storage (gp3)
          +€4 (€4 → €8)

+ aws_s3_bucket.logs
  +€20

    + Storage (S3)
      +€23, +1000 GB*

Monthly cost change for my-project (Module path: modules/compute, Workspace: prod)
Amount:  +€200 (EUR) (€300 → €500)
Percent: +67%

──────────────────────────────────
Project: my-project
Module path: modules/network
Workspace: prod

+ aws_nat_gateway.main
  +€100

Monthly cost change for my-project (Module path: modules/network, Workspace: prod)
Amount:  +€100 (EUR) (€200 → €300)
Percent: +50%

──────────────────────────────────
Key: * usage cost, ~ changed, + added, - removed

*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.

5 cloud resources were detected:
∙ 3 were estimated
∙ 1 was free
∙ 1 is not supported yet, see https://infracost.io/requested-resources:
  ∙ 1 x aws_acm_certificate
```
  </details>

</details>

<hr/>

<sub>
  This comment will be updated when code changes.
</sub>

