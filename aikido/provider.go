package aikido

import (
	"context"
	"os"

	"github.com/AikidoTerraform/terraform-provider-aikido/aikido/resources"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/auth"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &aikidoProvider{}

type aikidoProvider struct {
	version string
}

// aikidoProviderModel maps provider-block config to Go values.
type aikidoProviderModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	BaseURL      types.String `tfsdk:"base_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &aikidoProvider{version: version}
	}
}

func (p *aikidoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "aikido"
	resp.Version = p.version
}

func (p *aikidoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Manage Aikido Security resources via the Aikido REST API.",
		MarkdownDescription: "Manage [Aikido Security](https://www.aikido.dev/) resources via the [management API](https://apidocs.aikido.dev/).",
		Attributes: map[string]schema.Attribute{
			"client_id": schema.StringAttribute{
				Optional:    true,
				Description: "Aikido API client ID. Falls back to the AIKIDO_CLIENT_ID environment variable.",
			},
			"client_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Aikido API client secret. Falls back to the AIKIDO_CLIENT_SECRET environment variable.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Aikido API base URL. Set this to target a non-default region (e.g. AU, ME, US). Falls back to the AIKIDO_BASE_URL environment variable, then the default public API.",
			},
		},
	}
}

func (p *aikidoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config aikidoProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clientID := firstNonEmpty(config.ClientID.ValueString(), os.Getenv("AIKIDO_CLIENT_ID"))
	clientSecret := firstNonEmpty(config.ClientSecret.ValueString(), os.Getenv("AIKIDO_CLIENT_SECRET"))
	baseURL := firstNonEmpty(config.BaseURL.ValueString(), os.Getenv("AIKIDO_BASE_URL"), client.DefaultBaseURL)

	if clientID == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_id"),
			"Missing Aikido API client ID",
			"Set the client_id attribute or the AIKIDO_CLIENT_ID environment variable.",
		)
	}
	if clientSecret == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_secret"),
			"Missing Aikido API client secret",
			"Set the client_secret attribute or the AIKIDO_CLIENT_SECRET environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	httpClient := auth.NewHTTPClient(clientID, clientSecret, baseURL)
	c := client.New(httpClient, baseURL)

	// Make the configured client available to resources and data sources.
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *aikidoProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewRepositoryResource,
		resources.NewAutofixDependencySettingsResource,
		resources.NewAutofixSastSettingsResource,
		resources.NewAutofixPentestSettingsResource,
		resources.NewRepoPRChecksSettingsResource,
	}
}

func (p *aikidoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
