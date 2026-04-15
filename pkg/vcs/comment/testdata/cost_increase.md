<h3>💰 Infracost report</h3>
This pull request is aligned with your company's FinOps policies and the Well-Architected Framework.
<details >
  <summary><b>Monthly estimate increased by $50 📈</b> </summary>
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
      <td align="right">+$45</td>
      <td align="right">+$5</td>
      <td align="right">+$50 (+25%)</td>
      <td align="right">$250</td>
    </tr>
  </tbody>
</table>


*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.
  <details>
  <summary>Estimate details </summary>

```
Key: * usage cost, ~ changed, + added, - removed

──────────────────────────────────
Project: my-project

~ aws_instance.web
  +$50 ($200 → $250)

    ~ Linux/UNIX usage (on-demand, m5.xlarge)
      +$50 ($70 → $140)

Monthly cost change for my-project
Amount:  +$50 ($200 → $250)
Percent: +25%

──────────────────────────────────
Key: * usage cost, ~ changed, + added, - removed

*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.

3 cloud resources were detected:
∙ 3 were estimated
```
  </details>

</details>

<hr/>

<sub>
  This comment will be updated when code changes.
</sub>

